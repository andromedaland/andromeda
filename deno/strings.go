package deno

type RemoteModule interface {
	NameFromUrl(u string) string
}
