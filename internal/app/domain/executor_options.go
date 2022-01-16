package domain

type ExecutorOptions struct {
	WorkDir         string
	FallbackWorkDir string
	UID             string
	GID             string
	Username        string
	Env             map[string]string
}
