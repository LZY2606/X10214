package engine

const (
	SyntaxVersion  = "capture-traceroom-syntax-0.1.0"
	UnicodeVersion = "Unicode-15.0.0"
)

type Versions struct {
	Syntax  string `json:"syntax"`
	Unicode string `json:"unicode"`
}

func CurrentVersions() Versions {
	return Versions{Syntax: SyntaxVersion, Unicode: UnicodeVersion}
}
