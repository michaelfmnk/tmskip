// Package memfs is an in-memory fsys.FS used by unit tests: directories,
// files, symlinks, hard links, xattrs, an optional case-insensitive mode,
// injected errors and unmountable volumes.
package memfs

import (
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/michaelfmnk/tmskip/internal/fsys"
)

type node struct {
	typ      fsys.Type
	mode     os.FileMode
	children map[string]*node // dirs
	data     []byte           // files
	target   string           // symlinks
	xattrs   map[string][]byte
	dev, ino uint64
	nlink    uint64
}

// FS is the in-memory filesystem. It is safe for concurrent use.
type FS struct {
	mu        sync.Mutex
	root      *node
	ci        bool
	nextIno   uint64
	nextDev   uint64
	deny      map[string]syscall.Errno
	xfail     map[string]syscall.Errno // path + "\x00" + name
	ops       []string
	readDirs  []string
	writeFail map[string]syscall.Errno
	noSearch  map[string]syscall.Errno // DenySearch: real path → errno
}

// Option configures New.
type Option func(*FS)

// CaseInsensitive makes name lookups case-insensitive (like default APFS)
// while keeping names as stored.
func CaseInsensitive() Option { return func(m *FS) { m.ci = true } }

// New returns an empty filesystem containing only "/".
func New(opts ...Option) *FS {
	m := &FS{
		nextIno:   1,
		nextDev:   1,
		deny:      map[string]syscall.Errno{},
		xfail:     map[string]syscall.Errno{},
		writeFail: map[string]syscall.Errno{},
		noSearch:  map[string]syscall.Errno{},
	}
	m.root = m.newNode(fsys.TypeDir, 0o755, 1)
	for _, o := range opts {
		o(m)
	}
	return m
}

func (m *FS) newNode(t fsys.Type, mode os.FileMode, dev uint64) *node {
	m.nextIno++
	n := &node{typ: t, mode: mode, dev: dev, ino: m.nextIno, nlink: 1, xattrs: map[string][]byte{}}
	if t == fsys.TypeDir {
		n.children = map[string]*node{}
		n.nlink = 2
	}
	return n
}

func perr(op, p string, e syscall.Errno) error { return &fs.PathError{Op: op, Path: p, Err: e} }

func split(p string) []string {
	p = path.Clean("/" + p)
	if p == "/" {
		return nil
	}
	return strings.Split(p[1:], "/")
}

// child looks a name up in dir, honouring case-insensitivity. It returns the
// stored name.
func (m *FS) child(dir *node, name string) (*node, string) {
	if n, ok := dir.children[name]; ok {
		return n, name
	}
	if m.ci {
		for k, n := range dir.children {
			if strings.EqualFold(k, name) {
				return n, k
			}
		}
	}
	return nil, ""
}

// resolve walks p. Intermediate symlinks are always followed; the last one
// only when follow is true. It returns the node, its real path (stored-case
// names) and the parent dir node with the last stored name.
func (m *FS) resolve(op, p string, follow bool) (n *node, real string, err error) {
	hops := 0
	queue := split(p)
	cur := m.root
	curPath := "/"
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if cur.typ != fsys.TypeDir {
			return nil, "", perr(op, p, syscall.ENOTDIR)
		}
		if e, ok := m.noSearch[curPath]; ok {
			return nil, "", perr(op, p, e)
		}
		next, stored := m.child(cur, c)
		if next == nil {
			return nil, "", perr(op, p, syscall.ENOENT)
		}
		nextPath := path.Join(curPath, stored)
		if next.typ == fsys.TypeSymlink && (len(queue) > 0 || follow) {
			hops++
			if hops > 255 {
				return nil, "", perr(op, p, syscall.ELOOP)
			}
			t := next.target
			if !strings.HasPrefix(t, "/") {
				t = path.Join(curPath, t)
			}
			queue = append(split(t), queue...)
			cur, curPath = m.root, "/"
			continue
		}
		cur, curPath = next, nextPath
	}
	return cur, curPath, nil
}

// parentOf resolves the parent of p (following links) and returns it with
// the base name.
func (m *FS) parentOf(op, p string) (*node, string, string, error) {
	p = path.Clean(p)
	dir, base := path.Split(p)
	parent, real, err := m.resolve(op, dir, true)
	if err != nil {
		return nil, "", "", err
	}
	if parent.typ != fsys.TypeDir {
		return nil, "", "", perr(op, p, syscall.ENOTDIR)
	}
	if _, stored := m.child(parent, base); stored != "" {
		base = stored
	}
	return parent, base, path.Join(real, base), nil
}

func (m *FS) denied(op, p, real string) error {
	if e, ok := m.deny[real]; ok {
		return perr(op, p, e)
	}
	if e, ok := m.deny[path.Clean(p)]; ok {
		return perr(op, p, e)
	}
	return nil
}

func (m *FS) devFor(parent *node, parentPath string) uint64 {
	if parentPath == "/Volumes" {
		m.nextDev++
		return m.nextDev
	}
	return parent.dev
}

// ---- fsys.FS ----

func (m *FS) Lstat(p string) (fsys.Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, _, err := m.resolve("lstat", p, false)
	if err != nil {
		return fsys.Info{}, err
	}
	size := int64(len(n.data))
	if n.typ == fsys.TypeSymlink {
		size = int64(len(n.target))
	}
	blocks := (size + 4095) / 4096 * 8
	if n.typ == fsys.TypeDir {
		blocks = 0
	}
	return fsys.Info{Type: n.typ, Mode: n.mode, Size: size, Blocks: blocks, Dev: n.dev, Ino: n.ino, Nlink: n.nlink}, nil
}

func (m *FS) ReadDir(p string) ([]fsys.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readDirs = append(m.readDirs, path.Clean(p))
	n, real, err := m.resolve("readdir", p, true)
	if err != nil {
		return nil, err
	}
	if err := m.denied("readdir", p, real); err != nil {
		return nil, err
	}
	if e, ok := m.noSearch[real]; ok {
		return nil, perr("readdir", p, e)
	}
	if n.typ != fsys.TypeDir {
		return nil, perr("readdir", p, syscall.ENOTDIR)
	}
	out := make([]fsys.Entry, 0, len(n.children))
	for name, c := range n.children {
		out = append(out, fsys.Entry{Name: name, Type: c.typ})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *FS) Readlink(p string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, _, err := m.resolve("readlink", p, false)
	if err != nil {
		return "", err
	}
	if n.typ != fsys.TypeSymlink {
		return "", perr("readlink", p, syscall.EINVAL)
	}
	return n.target, nil
}

func (m *FS) xattrNode(op, p, name string) (*node, error) {
	n, real, err := m.resolve(op, p, false)
	if err != nil {
		return nil, err
	}
	if err := m.denied(op, p, real); err != nil {
		return nil, err
	}
	if e, ok := m.xfail[real+"\x00"+name]; ok {
		return nil, perr(op, p, e)
	}
	return n, nil
}

func (m *FS) GetXattr(p, name string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.xattrNode("getxattr", p, name)
	if err != nil {
		return nil, err
	}
	v, ok := n.xattrs[name]
	if !ok {
		return nil, fsys.ErrNoAttr
	}
	return append([]byte(nil), v...), nil
}

func (m *FS) SetXattr(p, name string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.xattrNode("setxattr", p, name)
	if err != nil {
		return err
	}
	n.xattrs[name] = append([]byte(nil), value...)
	m.ops = append(m.ops, "set "+name+" "+path.Clean(p))
	return nil
}

func (m *FS) RemoveXattr(p, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.xattrNode("removexattr", p, name)
	if err != nil {
		return err
	}
	if _, ok := n.xattrs[name]; ok {
		delete(n.xattrs, name)
		m.ops = append(m.ops, "rm "+name+" "+path.Clean(p))
	}
	return nil
}

func (m *FS) ReadFile(p string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, real, err := m.resolve("open", p, true)
	if err != nil {
		return nil, err
	}
	if err := m.denied("open", p, real); err != nil {
		return nil, err
	}
	if n.typ == fsys.TypeDir {
		return nil, perr("read", p, syscall.EISDIR)
	}
	return append([]byte(nil), n.data...), nil
}

func (m *FS) WriteFileAtomic(p string, data []byte, perm os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeLocked("open", p, data, perm, false)
}

func (m *FS) writeLocked(op, p string, data []byte, perm os.FileMode, appendMode bool) error {
	parent, base, real, err := m.parentOf(op, p)
	if err != nil {
		return err
	}
	if e, ok := m.writeFail[real]; ok {
		return perr(op, p, e)
	}
	if e, ok := m.deny[path.Dir(real)]; ok {
		return perr(op, p, e)
	}
	if old, ok := parent.children[base]; ok && old.typ == fsys.TypeDir {
		return perr(op, p, syscall.EISDIR)
	}
	if appendMode {
		if old, ok := parent.children[base]; ok && old.typ == fsys.TypeFile {
			old.data = append(old.data, data...)
			return nil
		}
	}
	n := m.newNode(fsys.TypeFile, perm.Perm(), m.devFor(parent, path.Dir(real)))
	n.data = append([]byte(nil), data...)
	parent.children[base] = n // atomic replace: a new inode
	return nil
}

func (m *FS) AppendFile(p string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeLocked("open", p, data, 0o644, true)
}

func (m *FS) MkdirAll(p string, perm os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mkdirAllLocked(p, perm)
}

func (m *FS) mkdirAllLocked(p string, perm os.FileMode) error {
	cur := m.root
	curPath := "/"
	for _, c := range split(p) {
		next, stored := m.child(cur, c)
		if next == nil {
			next = m.newNode(fsys.TypeDir, perm.Perm(), m.devFor(cur, curPath))
			cur.children[c] = next
			stored = c
		} else if next.typ == fsys.TypeSymlink {
			n, real, err := m.resolve("mkdir", path.Join(curPath, stored), true)
			if err != nil {
				return err
			}
			next, curPath, cur = n, real, n
			if next.typ != fsys.TypeDir {
				return perr("mkdir", p, syscall.ENOTDIR)
			}
			continue
		} else if next.typ != fsys.TypeDir {
			return perr("mkdir", p, syscall.ENOTDIR)
		}
		cur, curPath = next, path.Join(curPath, stored)
	}
	return nil
}

func (m *FS) Rename(oldp, newp string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	op, ob, _, err := m.parentOf("rename", oldp)
	if err != nil {
		return err
	}
	n, ok := op.children[ob]
	if !ok {
		return perr("rename", oldp, syscall.ENOENT)
	}
	np, nb, _, err := m.parentOf("rename", newp)
	if err != nil {
		return err
	}
	delete(op.children, ob)
	np.children[nb] = n
	return nil
}

func (m *FS) RemoveAll(p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	parent, base, _, err := m.parentOf("unlinkat", p)
	if err != nil {
		if fsys.IsNotExist(err) {
			return nil
		}
		return err
	}
	if n, ok := parent.children[base]; ok {
		if n.typ != fsys.TypeDir {
			n.nlink--
		}
		delete(parent.children, base)
	}
	return nil
}

func (m *FS) Probe(p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, real, err := m.resolve("open", p, true)
	if err != nil {
		return err
	}
	return m.denied("open", p, real)
}

func (m *FS) VolumeMounted(p string) bool {
	return fsys.VolumeMountedVia(m.Lstat, p)
}

// ---- test helpers (panic on misuse: they set up fixtures) ----

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// Mkdirs creates p and its parents (0755).
func (m *FS) Mkdirs(p string) { must(m.MkdirAll(p, 0o755)) }

// WriteFile creates a file (and its parent dirs) with data.
func (m *FS) WriteFile(p string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	must(m.mkdirAllLocked(path.Dir(p), 0o755))
	must(m.writeLocked("open", p, data, 0o644, false))
}

// Symlink creates link pointing at target (parents created).
func (m *FS) Symlink(target, link string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	must(m.mkdirAllLocked(path.Dir(link), 0o755))
	parent, base, real, err := m.parentOf("symlink", link)
	must(err)
	n := m.newNode(fsys.TypeSymlink, 0o755, m.devFor(parent, path.Dir(real)))
	n.target = target
	parent.children[base] = n
}

// Link creates a hard link newp to the file oldp.
func (m *FS) Link(oldp, newp string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, _, err := m.resolve("link", oldp, false)
	must(err)
	must(m.mkdirAllLocked(path.Dir(newp), 0o755))
	parent, base, _, err := m.parentOf("link", newp)
	must(err)
	n.nlink++
	parent.children[base] = n
}

// Deny makes ReadDir, GetXattr/SetXattr/RemoveXattr, ReadFile and Probe on
// exactly p fail with errno (Lstat still works, like TCC).
func (m *FS) Deny(p string, errno syscall.Errno) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := path.Clean(p)
	if _, real, err := m.resolve("deny", p, false); err == nil {
		key = real
	}
	m.deny[key] = errno
}

// DenySearch makes p behave like a folder without the search (x) bit, or
// one TCC protects: p itself can be lstat'ed, but listing it and reaching
// anything below it (Lstat, ReadDir, xattrs, …) fail with errno.
func (m *FS) DenySearch(p string, errno syscall.Errno) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := path.Clean(p)
	if _, real, err := m.resolve("denysearch", p, true); err == nil {
		key = real
	}
	m.noSearch[key] = errno
}

// FailWrite makes WriteFileAtomic/AppendFile of p fail with errno.
func (m *FS) FailWrite(p string, errno syscall.Errno) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeFail[path.Clean(p)] = errno
}

// FailXattr makes every xattr call for name on p fail with errno.
func (m *FS) FailXattr(p, name string, errno syscall.Errno) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := path.Clean(p)
	if _, real, err := m.resolve("failxattr", p, false); err == nil {
		key = real
	}
	m.xfail[key+"\x00"+name] = errno
}

// Unmount simulates unplugging the volume mounted at volumePath
// (/Volumes/<name>): the mount point disappears.
func (m *FS) Unmount(volumePath string) { must(m.RemoveAll(volumePath)) }

// Xattrs returns a copy of p's xattrs (p not followed); nil if p is missing.
func (m *FS) Xattrs(p string) map[string][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, _, err := m.resolve("xattrs", p, false)
	if err != nil {
		return nil
	}
	out := map[string][]byte{}
	for k, v := range n.xattrs {
		out[k] = append([]byte(nil), v...)
	}
	return out
}

// Ops is the log of SetXattr calls and RemoveXattr calls that removed something:
// "set <name> <path>" / "rm <name> <path>".
func (m *FS) Ops() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.ops...)
}

// ResetOps clears the xattr op log.
func (m *FS) ResetOps() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops = nil
}

// ReadDirCalls is the log of ReadDir paths, in call order.
func (m *FS) ReadDirCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.readDirs...)
}
