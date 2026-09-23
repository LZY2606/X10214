package syntax

import "unicode"

// SyntaxVersion 是本项目自有正则语法的版本钉（不依赖宿主正则库）。
const SyntaxVersion = "CTR-1.0"

// UnicodeDataVersion 是本项目执行模型所链接的 Unicode 表版本。
// 字符类的 \w \d \s 以及 \p{...} 的判定全部基于这些表，因此每次结果
// 与该版本绑定。
const UnicodeDataVersion = unicode.Version
