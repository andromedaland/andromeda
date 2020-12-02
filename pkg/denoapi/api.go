package denoapi

const CDN_HOST = "https://cdn.deno.land"
const API_HOST = "https://api.deno.land"

type SimpleModuleList []string

type Overview struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	StarCount   int    `json:"star_count"`
}

type Versions struct {
	Latest   string   `json:"latest"`
	Versions []string `json:"versions"`
}

type Meta struct {
	UploadedAt       string           `json:"uploaded_at"`
	DirectoryListing DirectoryListing `json:"directory_listing"`
}

type DirectoryListing struct {
	Path string `json:"path"`
	Size int    `json:"size"`
	Type string `json:"type"`
}

type DepsV2 struct {
	Graph NodeGraph `json:"graph"`
}

type NodeGraph struct {
	Nodes map[string]Node `json:"nodes"`
}

type Node struct {
	Size int      `json:"size"`
	Deps []string `json:"deps"`
}
