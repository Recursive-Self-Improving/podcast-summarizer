package transcribe

import "testing"

func TestIsMatchingPythonProcess(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "faster whisper helper", args: []string{"python3", "/tmp/faster_whisper_transcribe.py"}, want: true},
		{name: "transcribe script with flag", args: []string{"/usr/bin/python3.11", "-u", "/opt/transcribe.py"}, want: true},
		{name: "extract script", args: []string{"python", "extract.py", "--input", "audio.wav"}, want: true},
		{name: "non python", args: []string{"node", "extract.py"}, want: false},
		{name: "unrelated python", args: []string{"python3", "worker.py"}, want: false},
		{name: "script substring", args: []string{"python3", "my_transcribe.py"}, want: false},
		{name: "backup suffix", args: []string{"python3", "extract.py.bak"}, want: false},
		{name: "module", args: []string{"python3", "-m", "extract.py"}, want: false},
		{name: "command string", args: []string{"python3", "-c", "print('extract.py')"}, want: false},
		{name: "interpreter flag", args: []string{"python3", "-u", "faster_whisper_transcribe.py"}, want: true},
		{name: "flag argument", args: []string{"python3", "worker.py", "--note", "extract.py"}, want: false},
		{name: "shell command", args: []string{"bash", "-c", "python3 faster_whisper_transcribe.py"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isMatchingPythonProcess(test.args); got != test.want {
				t.Fatalf("isMatchingPythonProcess(%#v) = %v, want %v", test.args, got, test.want)
			}
		})
	}
}
