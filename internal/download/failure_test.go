package download

import (
	"testing"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/store"
)

func TestFailureSummary(t *testing.T) {
	for _, tc := range []struct {
		name, failure, evidence string
		imported                api.ImportState
		want                    string
	}{
		{"missing articles", "Aborted, cannot be completed", "Downloaded in 12 seconds<br/>27 articles were missing", "", "27 articles missing"},
		{"one missing article", "Aborted, cannot be completed", "1 article was missing", "", "1 article missing"},
		{"no missing articles", "Aborted, cannot be completed", "0 articles were missing", "", ""},
		{"no evidence", "Aborted, cannot be completed", "Downloaded in 12 seconds", "", ""},
		{"disk failure", "Unpacking failed: disk is full", "27 articles were missing", "", ""},
		{"unknown failure", "An unfamiliar server failure", "27 articles were missing", "", ""},
		{"file check", "file check: no German track", "27 articles were missing", store.ImportRejected, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := store.Download{State: store.Failed, Failure: tc.failure, Import: store.Import{State: tc.imported},
				Stages: []store.Stage{{Name: "Download", Actions: []string{tc.evidence}}}}
			if got := failureSummary(d); got != tc.want {
				t.Errorf("summary = %q, want %q", got, tc.want)
			}
			if d.Failure != tc.failure {
				t.Error("summarizing replaced the original error")
			}
		})
	}
}
