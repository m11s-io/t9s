package application

import (
	"errors"
	"testing"
)

func TestMutationRegistryExclusionAndRelease(t *testing.T) {
	for _, tc := range []struct {
		name, held, requested string
		blocked               bool
	}{
		{"same cluster", "cluster-a", "cluster-a", true},
		{"different clusters", "cluster-a", "cluster-b", false},
		{"unknown blocks known", "", "cluster-a", true},
		{"known blocks unknown", "cluster-a", "", true},
		{"unknown blocks unknown", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := mutationRegistry{known: make(map[string]uint32)}
			release, err := registry.acquire(tc.held)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(release)
			otherRelease, err := registry.acquire(tc.requested)
			if otherRelease != nil {
				t.Cleanup(otherRelease)
			}
			if tc.blocked {
				if !errors.Is(err, ErrMutationBusy) {
					t.Fatalf("concurrent admission = %v, want ErrMutationBusy", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				otherRelease()
			}
			release()
			release() // Releasing twice must not corrupt the registry.
			nextRelease, err := registry.acquire(tc.requested)
			if err != nil {
				t.Fatalf("admission after release: %v", err)
			}
			t.Cleanup(nextRelease)
		})
	}
}
