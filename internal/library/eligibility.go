package library

// TopicEvidence comes from a complete scan of the thread, not a video-only
// search. Images and plain text are not episode documents.
type TopicReference struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

type TopicEvidence struct {
	References            []TopicReference `json:"references,omitempty"`
	Complete              bool             `json:"complete"`
	ScanFailed            bool             `json:"scan_failed"`
	Videos                int              `json:"videos"`
	NonVideoEpisodes      int              `json:"nonvideo_episodes"`
	UnclassifiedDocuments int              `json:"unclassified_documents"`
	ChangedSinceScan      bool             `json:"changed_since_scan"`
}

// TopicEligibility gates creation of the destination and the poster together.
func TopicEligibility(e TopicEvidence) string {
	if e.ScanFailed {
		return "scan_failed"
	}
	if !e.Complete || e.ChangedSinceScan {
		return "pending"
	}
	if e.NonVideoEpisodes > 0 {
		return "excluded_nonvideo"
	}
	if e.UnclassifiedDocuments > 0 {
		return "review"
	}
	if e.Videos == 0 {
		return "empty"
	}
	return "eligible"
}
