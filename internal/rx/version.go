package rx

// UnicodeVersion is the Unicode database version whose tables back the
// character class / \p{} / \w semantics. It is the version shipped by the
// Go standard library's unicode package used to build this binary (verified
// to be Unicode 15.0 tables on this toolchain: scripts introduced in 16.0
// such as Garay are absent, 15.0 scripts such as Kawi are present).
const UnicodeVersion = "15.0.0"
