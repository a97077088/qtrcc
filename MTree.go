package main

type NodeData struct {
	Node       *QtResNode
	FileNode   *QtResFile
	FileData   *QtResFileData
	FileOffset uint64
}

type MNode struct {
	Tag      string
	Path     string
	Flags    uint16
	Val      *NodeData
	Children []*MNode
}

func TreeToMap(node *MNode, mp map[string]*NodeData) {
	for _, child := range node.Children {
		if child.Val.Node.Flags != 2 {
			mp[child.Path] = child.Val
		}
		if len(child.Children) > 0 {
			TreeToMap(child, mp)
		}
	}
}
