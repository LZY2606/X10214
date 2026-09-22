package syntax

// srcRange0 returns the source start offset of a node.
func srcLoOf(n Node) int {
	lo, _ := n.srcRange()
	return lo
}
