package regex

// VersionInfo pins the semantic grammar and the Unicode database used by
// the execution model. Every parse and match result carries these values.
type VersionInfo struct {
	GrammarVersion string
	UnicodeVersion string
}

// Versions returns the pinned versions for this build.
func Versions() VersionInfo {
	return VersionInfo{
		GrammarVersion: GrammarVersion,
		UnicodeVersion: UnicodeVersion,
	}
}

const (
	// GrammarVersion identifies the documented syntax/execution semantics.
	GrammarVersion = "CTRX-1.0.0"
	// UnicodeVersion mirrors the Unicode version of the Go unicode tables
	// this build links against. It is reported verbatim in every result.
	UnicodeVersion = unicodeVersion
)
