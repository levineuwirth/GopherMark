package ui

import "github.com/levineuwirth/gophermark/internal/models"

type TreeNode struct {
	Folder   *models.Bookmark
	Depth    int
	HasKids  bool
	Expanded bool
}

func BuildFlatTree(root *models.Bookmark, expandedFolders map[int64]bool) []*TreeNode {
	var nodes []*TreeNode

	var traverse func(*models.Bookmark, int)
	traverse = func(node *models.Bookmark, depth int) {
		if !node.IsFolder() {
			return
		}

		if depth > 0 {
			hasChildren := hasSubfolders(node)
			expanded := expandedFolders[node.ID]

			nodes = append(nodes, &TreeNode{
				Folder:   node,
				Depth:    depth - 1, // start visible folders at 0
				HasKids:  hasChildren,
				Expanded: expanded,
			})

			if !expanded {
				return
			}
		}

		for _, child := range node.Children {
			if child.IsFolder() {
				traverse(child, depth+1)
			}
		}
	}

	traverse(root, 0)
	return nodes
}

// hasSubfolders checks if a folder contains any subfolders
func hasSubfolders(folder *models.Bookmark) bool {
	for _, child := range folder.Children {
		if child.IsFolder() {
			return true
		}
	}
	return false
}

func FindBookmarksBar(root *models.Bookmark) *models.Bookmark {
	var find func(*models.Bookmark) *models.Bookmark
	find = func(node *models.Bookmark) *models.Bookmark {
		if node.IsFolder() && (node.Title == "Bookmarks Bar" || node.Title == "toolbar" || node.Title == "Bookmarks Toolbar") {
			return node
		}
		for _, child := range node.Children {
			if result := find(child); result != nil {
				return result
			}
		}
		return nil
	}
	return find(root)
}

func FindNodeIndex(nodes []*TreeNode, folderID int64) int {
	for i, node := range nodes {
		if node.Folder.ID == folderID {
			return i
		}
	}
	return -1
}

func ExpandPath(root *models.Bookmark, targetFolder *models.Bookmark, expandedFolders map[int64]bool) {
	path := findPath(root, targetFolder)

	for _, folder := range path {
		if folder.IsFolder() {
			expandedFolders[folder.ID] = true
		}
	}
}

func findPath(root *models.Bookmark, target *models.Bookmark) []*models.Bookmark {
	if root.ID == target.ID {
		return []*models.Bookmark{root}
	}

	for _, child := range root.Children {
		if child.IsFolder() {
			if path := findPath(child, target); path != nil {
				return append([]*models.Bookmark{root}, path...)
			}
		}
	}

	return nil
}
