package regex

// ProgramMeta describes one compiled pattern and is returned with every
// match / trace / diff result so the syntax grammar and Unicode database
// version are pinned.
type ProgramMeta struct {
	Pattern    string
	Flags      string
	NumGroups  int
	Names      map[string]int
	GroupNames []string
	SyntaxVer  string
	UnicodeVer string
}
