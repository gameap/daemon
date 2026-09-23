package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolveFixture is a work directory next to a second "drive":
//
//	work/servers/real/f.txt            plain server
//	work/servers/x -> disk2/servers/x  server kept on the other drive (allowed)
//	work/servers/bad -> secret         link an unprivileged user could make
//	disk2/servers/x/jump -> secret     same, but planted on the other drive
type resolveFixture struct {
	base    string
	work    string
	allowed string // disk2/servers
	secret  string
}

func newResolveFixture(t *testing.T) *resolveFixture {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privilege on Windows")
	}

	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	f := &resolveFixture{
		base:    base,
		work:    filepath.Join(base, "work"),
		allowed: filepath.Join(base, "disk2", "servers"),
		secret:  filepath.Join(base, "secret"),
	}

	f.mkdir(t, "work/servers/real")
	f.mkdir(t, "work/servers/deep/inner")
	f.mkdir(t, "disk2/servers/x/cfg")
	f.mkdir(t, "disk2/serversX")
	f.mkdir(t, "secret")
	f.write(t, "work/servers/real/f.txt", "real")
	f.write(t, "work/servers/B", "lexical")
	f.write(t, "work/servers/deep/B", "kernel")
	f.write(t, "disk2/servers/x/cfg/server.cfg", "cfg")
	f.write(t, "disk2/serversX/passwd", "SIMILAR")
	f.write(t, "secret/passwd", "TOPSECRET")

	f.link(t, "work/servers/x", filepath.Join(base, "disk2", "servers", "x"))
	f.link(t, "work/servers/bad", f.secret)
	f.link(t, "work/servers/similar", filepath.Join(base, "disk2", "serversX"))
	f.link(t, "work/servers/absin", filepath.Join(f.work, "servers", "real"))
	f.link(t, "work/servers/relin", "real")
	f.link(t, "work/servers/climb", "../../disk2/servers/x")
	f.link(t, "work/servers/parent", filepath.Join(base, "disk2"))
	f.link(t, "work/servers/root", f.allowed)
	f.link(t, "work/servers/A", "deep/inner")
	f.link(t, "work/servers/L", "A/../B")
	f.link(t, "disk2/servers/x/jump", f.secret)
	f.link(t, "disk2/servers/x/back", filepath.Join(f.work, "servers", "real"))
	// Lexically under the allowed directory, physically in secret: the hole a
	// resolver opens when it trusts the spelling of a link target.
	f.link(t, "work/servers/evil", filepath.Join(base, "disk2", "servers", "x", "jump"))

	// c1 -> c2 -> ... -> c9 -> real: one hop past the limit.
	for i := 1; i <= 9; i++ {
		target := "c" + strconv.Itoa(i+1)
		if i == 9 {
			target = "real"
		}

		f.link(t, "work/servers/c"+strconv.Itoa(i), target)
	}

	return f
}

func (f *resolveFixture) mkdir(t *testing.T, rel string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(f.base, filepath.FromSlash(rel)), 0o755))
}

func (f *resolveFixture) write(t *testing.T, rel, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(f.base, filepath.FromSlash(rel)), []byte(content), 0o644))
}

func (f *resolveFixture) link(t *testing.T, rel, target string) {
	t.Helper()
	require.NoError(t, os.Symlink(target, filepath.Join(f.base, filepath.FromSlash(rel))))
}

func TestResolver_Resolve(t *testing.T) {
	f := newResolveFixture(t)

	tests := []struct {
		name        string
		path        string
		leaf        LeafMode
		allowed     []string
		wantAnchor  string
		wantRel     string
		wantContent string // read through the returned root when set
		wantErr     string
	}{
		{
			name:       "plain_path",
			path:       "servers/real/f.txt",
			wantAnchor: f.work, wantRel: "servers/real/f.txt", wantContent: "real",
		},
		{
			name:       "root_dot",
			path:       ".",
			wantAnchor: f.work, wantRel: ".",
		},
		{
			name:       "absolute_link_into_allowed_target_reanchors",
			path:       "servers/x/cfg/server.cfg",
			wantAnchor: f.allowed, wantRel: "x/cfg/server.cfg", wantContent: "cfg",
		},
		{
			name:       "server_root_listing_follows_leaf",
			path:       "servers/x",
			leaf:       FollowLeaf,
			wantAnchor: f.allowed, wantRel: "x",
		},
		{
			name:       "no_follow_leaf_keeps_the_link",
			path:       "servers/x",
			leaf:       NoFollowLeaf,
			wantAnchor: f.work, wantRel: "servers/x",
		},
		{
			name:       "absolute_link_into_work_dir_is_followed",
			path:       "servers/absin/f.txt",
			wantAnchor: f.work, wantRel: "servers/real/f.txt", wantContent: "real",
		},
		{
			name:       "relative_link_inside_work_dir",
			path:       "servers/relin/f.txt",
			wantAnchor: f.work, wantRel: "servers/real/f.txt", wantContent: "real",
		},
		{
			name:       "relative_link_climbing_into_allowed_target",
			path:       "servers/climb/cfg/server.cfg",
			wantAnchor: f.allowed, wantRel: "x/cfg/server.cfg", wantContent: "cfg",
		},
		{
			name:       "link_to_the_allowed_root_itself",
			path:       "servers/root/x/cfg/server.cfg",
			wantAnchor: f.allowed, wantRel: "x/cfg/server.cfg", wantContent: "cfg",
		},
		{
			name:       "link_from_allowed_target_back_into_work_dir",
			path:       "servers/x/back/f.txt",
			wantAnchor: f.work, wantRel: "servers/real/f.txt", wantContent: "real",
		},
		{
			name:       "link_target_dotdot_is_resolved_like_the_kernel",
			path:       "servers/L",
			wantAnchor: f.work, wantRel: "servers/deep/B", wantContent: "kernel",
		},
		{
			name:       "missing_tail_is_appended",
			path:       "servers/x/newdir/sub",
			wantAnchor: f.allowed, wantRel: "x/newdir/sub",
		},
		{
			name:    "absolute_link_outside_every_root_refused",
			path:    "servers/bad/passwd",
			wantErr: "outside work directory",
		},
		{
			name:    "nested_hop_from_allowed_target_refused",
			path:    "servers/x/jump/passwd",
			wantErr: "outside work directory",
		},
		{
			name:    "link_target_spelled_under_allowed_target_but_through_a_planted_link_refused",
			path:    "servers/evil/passwd",
			wantErr: "outside work directory",
		},
		{
			name:    "prefix_similar_directory_refused",
			path:    "servers/similar/passwd",
			wantErr: "outside work directory",
		},
		{
			// Only the allowed directory itself is opened; the path is followed on
			// paper through its parent and enters the root where it should.
			name:       "link_to_parent_of_allowed_target_reaches_only_the_target",
			path:       "servers/parent/servers/x/cfg/server.cfg",
			wantAnchor: f.allowed, wantRel: "x/cfg/server.cfg", wantContent: "cfg",
		},
		{
			name:    "link_to_parent_of_allowed_target_refused_elsewhere",
			path:    "servers/parent/serversX/passwd",
			wantErr: "outside work directory",
		},
		{
			name:    "too_many_hops",
			path:    "servers/c1/f.txt",
			wantErr: "too many levels of symbolic links",
		},
		{
			name:    "dotdot_in_request_still_rejected",
			path:    "../secret/passwd",
			wantErr: "outside work directory",
		},
		{
			name:       "empty_list_does_not_walk",
			path:       "servers/x/cfg/server.cfg",
			allowed:    []string{},
			wantAnchor: f.work, wantRel: "servers/x/cfg/server.cfg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed := tt.allowed
			if allowed == nil {
				allowed = []string{f.allowed}
			}

			r := NewResolver(f.work, WithAllowedSymlinkTargets(allowed))

			res, err := r.Resolve(tt.path, tt.leaf)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				if tt.wantErr == "outside work directory" {
					assert.ErrorIs(t, err, ErrPathOutsideRoot, "the API maps this sentinel to a client error")
				}

				return
			}

			require.NoError(t, err)
			defer res.Close()

			assert.Equal(t, tt.wantAnchor, res.Anchor)
			assert.Equal(t, tt.wantRel, res.Rel)

			if tt.wantContent != "" {
				content, readErr := res.Root.ReadFile(res.Rel)
				require.NoError(t, readErr)
				assert.Equal(t, tt.wantContent, string(content))
			}
		})
	}
}

func TestResolver_RefusalNamesLinkTargetAndConfigKey(t *testing.T) {
	f := newResolveFixture(t)
	r := NewResolver(f.work, WithAllowedSymlinkTargets([]string{f.allowed}))

	_, err := r.Resolve("servers/bad/passwd", FollowLeaf)
	require.Error(t, err)

	var refused *SymlinkRefusedError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, filepath.Join(f.work, "servers", "bad"), refused.Link)
	assert.Equal(t, f.secret, refused.Target)
	assert.Contains(t, err.Error(), "allowed_symlink_targets")
	assert.True(t, errors.Is(err, ErrPathOutsideRoot))
}

func TestResolver_SameAnchor(t *testing.T) {
	f := newResolveFixture(t)
	r := NewResolver(f.work, WithAllowedSymlinkTargets([]string{f.allowed}))

	inWork, err := r.Resolve("servers/real/f.txt", NoFollowLeaf)
	require.NoError(t, err)
	defer inWork.Close()

	onDisk2, err := r.Resolve("servers/x/cfg/server.cfg", NoFollowLeaf)
	require.NoError(t, err)
	defer onDisk2.Close()

	onDisk2Again, err := r.Resolve("servers/x/cfg", NoFollowLeaf)
	require.NoError(t, err)
	defer onDisk2Again.Close()

	assert.False(t, inWork.SameAnchor(onDisk2))
	assert.True(t, onDisk2.SameAnchor(onDisk2Again))
}

func TestResolver_WorkDirUnavailableKeepsMarker(t *testing.T) {
	r := NewResolver(filepath.Join(t.TempDir(), "missing"))

	_, err := r.Resolve("x", FollowLeaf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "work directory unavailable")
}

func TestResolver_AllowedTargetUnavailableKeepsMarker(t *testing.T) {
	f := newResolveFixture(t)
	missing := filepath.Join(f.base, "unmounted")
	f.link(t, "work/servers/unmounted", filepath.Join(missing, "x"))

	r := NewResolver(f.work, WithAllowedSymlinkTargets([]string{missing}))

	_, err := r.Resolve("servers/unmounted/f.txt", FollowLeaf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "work directory unavailable")
	assert.Contains(t, err.Error(), missing)
}

// TestResolver_ResolvedSpelling covers an allowed target configured through
// a symlinked parent (macOS /var -> /private/var, or /srv -> /data/srv): a
// link written with the resolved spelling must still be recognised.
func TestResolver_ResolvedSpelling(t *testing.T) {
	f := newResolveFixture(t)

	alias := filepath.Join(f.base, "alias")
	f.link(t, "alias", filepath.Join(f.base, "disk2"))
	f.link(t, "work/servers/viaresolved", filepath.Join(f.base, "disk2", "servers", "x"))

	r := NewResolver(f.work, WithAllowedSymlinkTargets([]string{filepath.Join(alias, "servers")}))

	res, err := r.Resolve("servers/viaresolved/cfg/server.cfg", FollowLeaf)
	require.NoError(t, err)
	defer res.Close()

	assert.Equal(t, filepath.Join(alias, "servers"), res.Anchor)

	content, err := res.Root.ReadFile(res.Rel)
	require.NoError(t, err)
	assert.Equal(t, "cfg", string(content))
}
