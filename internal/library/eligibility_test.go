package library

import "testing"

func TestWholeTopicEligibility(t *testing.T) {
	for _, tc := range []struct {
		evidence TopicEvidence
		want     string
	}{
		{TopicEvidence{Complete: true, Videos: 10, NonVideoEpisodes: 1}, "excluded_nonvideo"},
		{TopicEvidence{Complete: true, Videos: 10}, "eligible"},
		{TopicEvidence{Videos: 10}, "pending"},
		{TopicEvidence{Complete: true, Videos: 10, ChangedSinceScan: true}, "pending"},
		{TopicEvidence{Complete: true, Videos: 10, UnclassifiedDocuments: 1}, "review"},
		{TopicEvidence{Complete: true, NonVideoEpisodes: 1}, "excluded_nonvideo"},
		{TopicEvidence{Complete: true}, "empty"},
		{TopicEvidence{Videos: 10, ScanFailed: true}, "scan_failed"},
	} {
		if got := TopicEligibility(tc.evidence); got != tc.want {
			t.Fatalf("%+v: %s want %s", tc.evidence, got, tc.want)
		}
	}
}
