package provider

import "fmt"

type Registry struct {
	providers []Provider
}

func NewRegistry(providers ...Provider) Registry {
	return Registry{providers: providers}
}

func DefaultRegistry() Registry {
	return NewRegistry(YouTube{}, XiaoyuzhouFM{}, SoundOnFM{})
}

func (r Registry) Find(rawURL string) (Provider, MediaRef, error) {
	for _, provider := range r.providers {
		if provider.Match(rawURL) {
			ref, err := provider.Normalize(rawURL)
			if err != nil {
				return nil, MediaRef{}, fmt.Errorf("%w: %w", ErrInvalidURL, err)
			}
			return provider, ref, nil
		}
	}
	return nil, MediaRef{}, fmt.Errorf("%w: no provider matched URL", ErrInvalidURL)
}
