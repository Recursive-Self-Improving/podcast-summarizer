package queue

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/db"
)

func TestWorkerProcessesQueuedJobsSerially(t *testing.T) {
	repo := newWorkerRepo()
	first := repo.addJob(db.TranscriptionJob{MediaItemID: 1, Status: "queued"})
	second := repo.addJob(db.TranscriptionJob{MediaItemID: 2, Status: "queued"})
	processor := &fakeProcessor{results: []Transcript{{Source: "whisper", Text: "first transcript"}, {Source: "whisper", Text: "second transcript"}}}
	worker := Worker{Repo: repo, Processor: processor, RequestProcessor: &fakeRequestProcessor{}}

	for i := 0; i < 2; i++ {
		didWork, err := worker.RunOnce(context.Background())
		if err != nil {
			t.Fatalf("RunOnce returned error: %v", err)
		}
		if !didWork {
			t.Fatal("RunOnce did not process a job")
		}
	}
	if !slices.Equal(processor.jobIDs, []int64{first.ID, second.ID}) {
		t.Fatalf("jobIDs = %#v", processor.jobIDs)
	}
	if repo.jobs[first.ID].Status != "completed" || repo.jobs[second.ID].Status != "completed" {
		t.Fatalf("jobs = %#v", repo.jobs)
	}
	wantStatuses := []string{"downloading_audio", "converting_audio", "splitting_audio", "transcribing", "transcript_ready"}
	if !slices.Equal(repo.mediaStatuses[first.MediaItemID], wantStatuses) {
		t.Fatalf("media statuses = %#v", repo.mediaStatuses[first.MediaItemID])
	}
}

func TestWorkerRetriesOnceThenMarksFailedAndNotifies(t *testing.T) {
	repo := newWorkerRepo()
	job := repo.addJob(db.TranscriptionJob{MediaItemID: 1, Status: "queued"})
	repo.requests[job.MediaItemID] = []db.SummaryRequest{{ID: 100, ChatID: 10}, {ID: 101, ChatID: 11}}
	processor := &fakeProcessor{err: errors.New("transcription failed")}
	notifier := &fakeNotifier{}
	worker := Worker{Repo: repo, Processor: processor, Notifier: notifier, RequestProcessor: &fakeRequestProcessor{}, MaxAttempts: 2}

	didWork, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("first RunOnce returned error: %v", err)
	}
	if !didWork || repo.jobs[job.ID].Status != "queued" || repo.jobs[job.ID].AttemptCount != 1 {
		t.Fatalf("job after retry = %#v", repo.jobs[job.ID])
	}
	if got := repo.mediaStatuses[job.MediaItemID][len(repo.mediaStatuses[job.MediaItemID])-1]; got != "queued" {
		t.Fatalf("last media status after retry = %q", got)
	}
	if len(notifier.messages) != 0 {
		t.Fatalf("messages = %#v", notifier.messages)
	}

	didWork, err = worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("second RunOnce returned error: %v", err)
	}
	if !didWork || repo.jobs[job.ID].Status != "failed" || repo.jobs[job.ID].AttemptCount != 2 {
		t.Fatalf("job after failure = %#v", repo.jobs[job.ID])
	}
	if len(notifier.messages) != 2 || notifier.messages[0] != "Transcription failed. Please try again later." {
		t.Fatalf("messages = %#v", notifier.messages)
	}
	if repo.requestFailures[100] != "transcription failed" || repo.requestFailures[101] != "transcription failed" {
		t.Fatalf("request failures = %#v", repo.requestFailures)
	}
}

func TestWorkerCancellationRequeuesWithoutIncrementingAttempt(t *testing.T) {
	repo := newWorkerRepo()
	job := repo.addJob(db.TranscriptionJob{MediaItemID: 1, Status: "queued", AttemptCount: 1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worker := Worker{Repo: repo, Processor: &fakeProcessor{err: context.Canceled}, RequestProcessor: &fakeRequestProcessor{}, MaxAttempts: 2}

	didWork, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if !didWork || repo.jobs[job.ID].Status != "queued" || repo.jobs[job.ID].AttemptCount != 1 {
		t.Fatalf("job = %#v", repo.jobs[job.ID])
	}
	if got := repo.mediaStatuses[job.MediaItemID][len(repo.mediaStatuses[job.MediaItemID])-1]; got != "queued" {
		t.Fatalf("last media status after cancellation = %q", got)
	}
}

func TestWorkerProcessorDeadlineExceededConsumesAttempt(t *testing.T) {
	repo := newWorkerRepo()
	job := repo.addJob(db.TranscriptionJob{MediaItemID: 1, Status: "queued"})
	worker := Worker{Repo: repo, Processor: &fakeProcessor{err: context.DeadlineExceeded}, RequestProcessor: &fakeRequestProcessor{}, MaxAttempts: 2}

	didWork, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if !didWork || repo.jobs[job.ID].Status != "queued" || repo.jobs[job.ID].AttemptCount != 1 {
		t.Fatalf("job = %#v", repo.jobs[job.ID])
	}
}

func TestWorkerCancellationDuringStatusUpdateRequeuesWithoutIncrementingAttempt(t *testing.T) {
	repo := newWorkerRepo()
	job := repo.addJob(db.TranscriptionJob{MediaItemID: 1, Status: "queued"})
	ctx, cancel := context.WithCancel(context.Background())
	repo.cancelOnStatusUpdate = cancel
	worker := Worker{Repo: repo, Processor: &fakeProcessor{}, RequestProcessor: &fakeRequestProcessor{}, MaxAttempts: 2}

	didWork, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if !didWork || repo.jobs[job.ID].Status != "queued" || repo.jobs[job.ID].AttemptCount != 0 {
		t.Fatalf("job = %#v", repo.jobs[job.ID])
	}
}

func TestWorkerCancellationDuringCompletionPersistsTranscript(t *testing.T) {
	repo := newWorkerRepo()
	job := repo.addJob(db.TranscriptionJob{MediaItemID: 1, Status: "queued"})
	ctx, cancel := context.WithCancel(context.Background())
	repo.cancelOnComplete = cancel
	requestProcessor := &fakeRequestProcessor{}
	worker := Worker{Repo: repo, Processor: &fakeProcessor{}, RequestProcessor: requestProcessor, MaxAttempts: 2}

	didWork, err := worker.RunOnce(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOnce error = %v", err)
	}
	if !didWork || repo.jobs[job.ID].Status != "completed" {
		t.Fatalf("job = %#v", repo.jobs[job.ID])
	}
	if len(requestProcessor.mediaIDs) != 0 {
		t.Fatalf("processed media IDs = %#v", requestProcessor.mediaIDs)
	}
}

func TestWorkerProcessesPendingRequestsAfterCompletion(t *testing.T) {
	repo := newWorkerRepo()
	job := repo.addJob(db.TranscriptionJob{MediaItemID: 1, Status: "queued"})
	requestProcessor := &fakeRequestProcessor{}
	worker := Worker{Repo: repo, Processor: &fakeProcessor{}, RequestProcessor: requestProcessor, MaxAttempts: 2}

	didWork, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if !didWork || repo.jobs[job.ID].Status != "completed" {
		t.Fatalf("job = %#v", repo.jobs[job.ID])
	}
	if !slices.Equal(requestProcessor.mediaIDs, []int64{job.MediaItemID}) {
		t.Fatalf("processed media IDs = %#v", requestProcessor.mediaIDs)
	}
}

func TestWorkerIgnoresPendingRequestProcessingError(t *testing.T) {
	repo := newWorkerRepo()
	job := repo.addJob(db.TranscriptionJob{MediaItemID: 1, Status: "queued"})
	requestProcessor := &fakeRequestProcessor{err: errors.New("summary failed")}
	worker := Worker{Repo: repo, Processor: &fakeProcessor{}, RequestProcessor: requestProcessor, MaxAttempts: 2}

	didWork, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if !didWork || repo.jobs[job.ID].Status != "completed" {
		t.Fatalf("job = %#v", repo.jobs[job.ID])
	}
}

func TestWorkerContinuesAfterPendingRequestProcessingError(t *testing.T) {
	repo := newWorkerRepo()
	first := repo.addJob(db.TranscriptionJob{MediaItemID: 1, Status: "queued"})
	second := repo.addJob(db.TranscriptionJob{MediaItemID: 2, Status: "queued"})
	requestProcessor := &fakeRequestProcessor{err: errors.New("summary failed")}
	worker := Worker{Repo: repo, Processor: &fakeProcessor{}, RequestProcessor: requestProcessor, MaxAttempts: 2}

	for i := 0; i < 2; i++ {
		didWork, err := worker.RunOnce(context.Background())
		if err != nil {
			t.Fatalf("RunOnce returned error: %v", err)
		}
		if !didWork {
			t.Fatal("RunOnce did not process a job")
		}
	}
	if repo.jobs[first.ID].Status != "completed" || repo.jobs[second.ID].Status != "completed" {
		t.Fatalf("jobs = %#v", repo.jobs)
	}
}

func TestWorkerCancellationWithProcessKillRequeuesWithoutAttempt(t *testing.T) {
	repo := newWorkerRepo()
	job := repo.addJob(db.TranscriptionJob{MediaItemID: 1, Status: "queued"})
	ctx, cancel := context.WithCancel(context.Background())
	worker := Worker{Repo: repo, Processor: &fakeProcessor{err: errors.New("signal: killed"), cancel: cancel}, RequestProcessor: &fakeRequestProcessor{}, MaxAttempts: 2}

	didWork, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if !didWork || repo.jobs[job.ID].Status != "queued" || repo.jobs[job.ID].AttemptCount != 0 {
		t.Fatalf("job = %#v", repo.jobs[job.ID])
	}
}

func TestWorkerProcessesPendingRequestsWhenFailureFindsTranscriptReadyMedia(t *testing.T) {
	repo := newWorkerRepo()
	job := repo.addJob(db.TranscriptionJob{MediaItemID: 1, Status: "queued", AttemptCount: 1})
	repo.requests[job.MediaItemID] = []db.SummaryRequest{{ID: 100, ChatID: 10}}
	repo.transcriptReadyOnFailure = true
	notifier := &fakeNotifier{}
	requestProcessor := &fakeRequestProcessor{}
	worker := Worker{Repo: repo, Processor: &fakeProcessor{err: errors.New("transcription failed")}, Notifier: notifier, RequestProcessor: requestProcessor, MaxAttempts: 2}

	didWork, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if !didWork || repo.jobs[job.ID].Status != "completed" {
		t.Fatalf("job = %#v", repo.jobs[job.ID])
	}
	if len(notifier.messages) != 0 || len(repo.requestFailures) != 0 {
		t.Fatalf("messages=%#v requestFailures=%#v", notifier.messages, repo.requestFailures)
	}
	if !slices.Equal(requestProcessor.mediaIDs, []int64{job.MediaItemID}) {
		t.Fatalf("processed media IDs = %#v", requestProcessor.mediaIDs)
	}
}

func TestWorkerWatchFailureUsesProgressFinalFailure(t *testing.T) {
	repo := newWorkerRepo()
	job := repo.addJob(db.TranscriptionJob{MediaItemID: 1, Status: "queued", AttemptCount: 1})
	repo.requests[job.MediaItemID] = []db.SummaryRequest{{ID: 100, ChatID: 10, MessageID: 0}}
	progress := &fakeProgressReporter{}
	worker := Worker{Repo: repo, Processor: &fakeProcessor{err: errors.New("transcription failed")}, Progress: progress, RequestProcessor: &fakeRequestProcessor{}, MaxAttempts: 2}

	didWork, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if !didWork || repo.jobs[job.ID].Status != "failed" {
		t.Fatalf("job = %#v", repo.jobs[job.ID])
	}
	if len(progress.mediaProgress) != 4 {
		t.Fatalf("media progress = %#v", progress.mediaProgress)
	}
	if len(progress.failures) != 1 || progress.failures[0].MessageID != 0 || progress.failureTexts[0] != "Transcription failed. Please try again later." {
		t.Fatalf("failures=%#v texts=%#v", progress.failures, progress.failureTexts)
	}
}

func TestWorkerIgnoresTerminalFailureNotificationErrors(t *testing.T) {
	repo := newWorkerRepo()
	job := repo.addJob(db.TranscriptionJob{MediaItemID: 1, Status: "queued", AttemptCount: 1})
	repo.requests[job.MediaItemID] = []db.SummaryRequest{{ID: 100, ChatID: 10}}
	worker := Worker{Repo: repo, Processor: &fakeProcessor{err: errors.New("transcription failed")}, Notifier: &fakeNotifier{err: errors.New("telegram failed")}, RequestProcessor: &fakeRequestProcessor{}, MaxAttempts: 2}

	didWork, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if !didWork || repo.jobs[job.ID].Status != "failed" {
		t.Fatalf("job = %#v", repo.jobs[job.ID])
	}
	if repo.requestFailures[100] != "transcription failed" {
		t.Fatalf("request failures = %#v", repo.requestFailures)
	}
}

func TestWorkerRunExitsWhenContextCancelled(t *testing.T) {
	repo := newWorkerRepo()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worker := Worker{Repo: repo, Processor: &fakeProcessor{}, RequestProcessor: &fakeRequestProcessor{}, IdleWait: time.Millisecond}

	err := worker.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v", err)
	}
}

type workerRepo struct {
	nextJobID                int64
	jobs                     map[int64]db.TranscriptionJob
	queue                    []int64
	mediaStatuses            map[int64][]string
	requests                 map[int64][]db.SummaryRequest
	requestFailures          map[int64]string
	cancelOnStatusUpdate     context.CancelFunc
	cancelOnComplete         context.CancelFunc
	transcriptReadyOnFailure bool
}

func newWorkerRepo() *workerRepo {
	return &workerRepo{
		nextJobID:       1,
		jobs:            map[int64]db.TranscriptionJob{},
		mediaStatuses:   map[int64][]string{},
		requests:        map[int64][]db.SummaryRequest{},
		requestFailures: map[int64]string{},
	}
}

func (r *workerRepo) addJob(job db.TranscriptionJob) db.TranscriptionJob {
	job.ID = r.nextJobID
	r.nextJobID++
	r.jobs[job.ID] = job
	if job.Status == "queued" {
		r.queue = append(r.queue, job.ID)
	}
	return job
}

func (r *workerRepo) ClaimOldestQueuedTranscriptionJob(_ context.Context) (db.TranscriptionJob, bool, error) {
	for len(r.queue) > 0 {
		jobID := r.queue[0]
		r.queue = r.queue[1:]
		job := r.jobs[jobID]
		if job.Status != "queued" {
			continue
		}
		job.Status = "downloading_audio"
		r.jobs[job.ID] = job
		r.mediaStatuses[job.MediaItemID] = append(r.mediaStatuses[job.MediaItemID], job.Status)
		return job, true, nil
	}
	return db.TranscriptionJob{}, false, nil
}

func (r *workerRepo) UpdateTranscriptionJobAndMediaStatus(ctx context.Context, jobID, mediaItemID int64, status string) error {
	if r.cancelOnStatusUpdate != nil && status != "queued" {
		r.cancelOnStatusUpdate()
		r.cancelOnStatusUpdate = nil
		return ctx.Err()
	}
	job := r.jobs[jobID]
	job.Status = status
	r.jobs[jobID] = job
	r.mediaStatuses[mediaItemID] = append(r.mediaStatuses[mediaItemID], status)
	return nil
}

func (r *workerRepo) CompleteTranscriptionJobWithTranscript(ctx context.Context, jobID, mediaItemID int64, _, _ string) error {
	if r.cancelOnComplete != nil {
		r.cancelOnComplete()
		r.cancelOnComplete = nil
		return ctx.Err()
	}
	job := r.jobs[jobID]
	job.Status = "completed"
	r.jobs[jobID] = job
	r.mediaStatuses[mediaItemID] = append(r.mediaStatuses[mediaItemID], "transcript_ready")
	return nil
}

func (r *workerRepo) MarkTranscriptionJobRetryQueued(ctx context.Context, jobID, mediaItemID int64, lastError string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	job := r.jobs[jobID]
	job.Status = "queued"
	job.AttemptCount++
	job.LastError = lastError
	r.jobs[jobID] = job
	r.queue = append(r.queue, jobID)
	r.mediaStatuses[mediaItemID] = append(r.mediaStatuses[mediaItemID], "queued")
	return nil
}

func (r *workerRepo) MarkTranscriptionJobFailedAndMediaFailed(ctx context.Context, jobID, mediaItemID int64, lastError string) (bool, []db.SummaryRequest, error) {
	if err := ctx.Err(); err != nil {
		return false, nil, err
	}
	job := r.jobs[jobID]
	if r.transcriptReadyOnFailure {
		job.Status = "completed"
		job.LastError = lastError
		r.jobs[jobID] = job
		r.mediaStatuses[mediaItemID] = append(r.mediaStatuses[mediaItemID], "transcript_ready")
		return false, nil, nil
	}
	job.Status = "failed"
	job.AttemptCount++
	job.LastError = lastError
	r.jobs[jobID] = job
	r.mediaStatuses[mediaItemID] = append(r.mediaStatuses[mediaItemID], "failed")
	failedRequests := append([]db.SummaryRequest(nil), r.requests[mediaItemID]...)
	for _, request := range failedRequests {
		r.requestFailures[request.ID] = lastError
	}
	return true, failedRequests, nil
}

type fakeProcessor struct {
	results []Transcript
	err     error
	cancel  context.CancelFunc
	jobIDs  []int64
}

func (f *fakeProcessor) Process(_ context.Context, job db.TranscriptionJob) (Transcript, error) {
	f.jobIDs = append(f.jobIDs, job.ID)
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	if f.err != nil {
		return Transcript{}, f.err
	}
	if len(f.results) == 0 {
		return Transcript{Source: "whisper", Text: "transcript"}, nil
	}
	result := f.results[0]
	f.results = f.results[1:]
	return result, nil
}

type fakeRequestProcessor struct {
	err      error
	mediaIDs []int64
}

func (f *fakeRequestProcessor) ProcessPendingRequests(_ context.Context, mediaItemID int64) error {
	f.mediaIDs = append(f.mediaIDs, mediaItemID)
	return f.err
}

type fakeProgressReporter struct {
	mediaProgress []string
	failures      []db.SummaryRequest
	failureTexts  []string
}

func (f *fakeProgressReporter) MediaProgress(_ context.Context, _ int64, text string) error {
	f.mediaProgress = append(f.mediaProgress, text)
	return nil
}

func (f *fakeProgressReporter) FinalFailureText(_ context.Context, request db.SummaryRequest, text string) error {
	f.failures = append(f.failures, request)
	f.failureTexts = append(f.failureTexts, text)
	return nil
}

type fakeNotifier struct {
	err      error
	messages []string
}

func (f *fakeNotifier) SendText(_ context.Context, _ int64, text string) error {
	if f.err != nil {
		return f.err
	}
	f.messages = append(f.messages, text)
	return nil
}
