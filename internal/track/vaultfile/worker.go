package vaultfile

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/ttak0422/track/internal/track/notedata"
	"github.com/ttak0422/track/internal/track/task"
	"gopkg.in/yaml.v3"
)

const workerArg = "--track-internal-read-worker-v1"

type request struct {
	Op, Path, Meta, Kind     string
	ID                       int64
	BodyVersion, MetaVersion fileInfo
	MetaFound                bool
}
type fileInfo struct {
	Base          string
	Bytes         int64
	Bits          uint32
	Modified      int64
	Device, Inode uint64
}

func (i fileInfo) Name() string       { return i.Base }
func (i fileInfo) Size() int64        { return i.Bytes }
func (i fileInfo) Mode() os.FileMode  { return os.FileMode(i.Bits) }
func (i fileInfo) ModTime() time.Time { return time.Unix(0, i.Modified) }
func (i fileInfo) IsDir() bool        { return i.Mode().IsDir() }
func (i fileInfo) Sys() any           { return nil }
func infoOf(i os.FileInfo) fileInfo {
	v := fileInfo{Base: i.Name(), Bytes: i.Size(), Bits: uint32(i.Mode()), Modified: i.ModTime().UnixNano()}
	if st, ok := i.Sys().(*syscall.Stat_t); ok {
		v.Device = uint64(st.Dev)
		v.Inode = uint64(st.Ino)
	}
	return v
}

type dirEntry struct{ fileInfo }

func (d dirEntry) Type() os.FileMode          { return d.Mode().Type() }
func (d dirEntry) Info() (os.FileInfo, error) { return d.fileInfo, nil }

type NoteRead struct {
	Body               string
	Metadata           []byte
	Found, Legacy      bool
	BodyInfo, MetaInfo fileInfo
	Tasks              []task.Task
}
type response struct {
	Data               []byte
	Info               fileInfo
	Entries            []dirEntry
	Resolved           string
	Note               *NoteRead
	Errno              int
	Problem, ErrorPath string
	Invalid            bool
}

func (r response) inputError(path string) error {
	if r.Problem == "" {
		return nil
	}
	if r.ErrorPath != "" {
		path = r.ErrorPath
	}
	if r.Invalid {
		return &ParseFailure{Path: path, Reason: r.Problem}
	}
	var err error = errors.New(r.Problem)
	if r.Errno != 0 {
		err = &os.PathError{Op: "read", Path: path, Err: syscall.Errno(r.Errno)}
	}
	return &Error{Path: path, Reason: r.Problem, Err: err}
}

type ParseFailure struct{ Path, Reason string }

func (e *ParseFailure) Error() string { return e.Path + ": " + e.Reason }
func frameRead(r io.Reader, v any) error {
	var size uint32
	if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
		return err
	}
	if size > maxFrame {
		return errors.New("read worker frame exceeds budget")
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
func frameWrite(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(data) > maxFrame {
		return errors.New("read worker frame exceeds budget")
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(data))); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// The hidden child exits before any configuration loading, CLI routing or writers run.
func init() {
	if len(os.Args) == 2 && os.Args[1] == workerArg && os.Getenv("TRACK_INTERNAL_READ_WORKER") == "1" {
		for {
			var r request
			if err := frameRead(os.Stdin, &r); err != nil {
				os.Exit(0)
			}
			v := perform(r)
			if err := frameWrite(os.Stdout, v); err != nil {
				os.Exit(1)
			}
		}
	}
}
func perform(r request) (v response) {
	if len(r.Path) > 8192 || len(r.Meta) > 8192 || r.Path == "" {
		v.Problem = "invalid read worker path"
		return
	}
	var err error
	switch r.Op {
	case "read":
		var f *os.File
		f, v.Data, err = read(r.Path)
		if f != nil {
			f.Close()
		}
	case "stat", "lstat":
		var i os.FileInfo
		if r.Op == "stat" {
			i, err = os.Stat(r.Path)
		} else {
			i, err = os.Lstat(r.Path)
		}
		if err == nil {
			v.Info = infoOf(i)
		}
	case "directory":
		var f *os.File
		f, err = os.Open(r.Path)
		if err == nil {
			var entries []os.DirEntry
			entries, err = f.ReadDir(100001)
			f.Close()
			if errors.Is(err, io.EOF) {
				err = nil
			}
			if len(entries) > 100000 {
				err = errors.New("directory entry budget exceeded")
			}
			if err == nil {
				for _, e := range entries {
					v.Entries = append(v.Entries, dirEntry{fileInfo{Base: e.Name(), Bits: uint32(e.Type())}})
				}
				sort.Slice(v.Entries, func(i, j int) bool { return v.Entries[i].Base < v.Entries[j].Base })
			}
		}
	case "canonical":
		v.Resolved, err = filepath.EvalSymlinks(r.Path)
	case "verify":
		var body, meta os.FileInfo
		body, err = os.Stat(r.Path)
		if err == nil && infoOf(body) != r.BodyVersion {
			err = errors.New("body changed since staging")
		}
		if err == nil {
			meta, err = os.Stat(r.Meta)
			if errors.Is(err, os.ErrNotExist) && !r.MetaFound {
				if _, linkErr := os.Lstat(r.Meta); os.IsNotExist(linkErr) {
					err = nil
				} else {
					err = errors.New("metadata path changed since staging")
				}
			} else if err == nil && (!r.MetaFound || infoOf(meta) != r.MetaVersion) {
				err = errors.New("metadata changed since staging")
			}
		}
	case "note":
		v.Note, err = parseNote(r)
	default:
		err = errors.New("invalid read worker operation")
	}
	if err != nil {
		v = response{Problem: boundedMessage(err.Error())}
		var local *Error
		var invalid *ParseFailure
		var errno syscall.Errno
		if errors.As(err, &local) {
			v.Problem = local.Reason
			v.ErrorPath = local.Path
		}
		if errors.As(err, &invalid) {
			v.Problem = invalid.Reason
			v.ErrorPath = invalid.Path
			v.Invalid = true
		}
		if errors.As(err, &errno) {
			v.Errno = int(errno)
		}
	}
	return
}
func stableRead(path string) ([]byte, fileInfo, error) {
	before, err := os.Stat(path)
	if err != nil {
		return nil, fileInfo{}, ReadError(path, err)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fileInfo{}, ReadError(path, err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, fileInfo{}, ReadError(path, err)
	}
	if !opened.Mode().IsRegular() {
		return nil, fileInfo{}, &Error{Path: path, Reason: "not a regular text file"}
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxReadBytes+1))
	if err != nil {
		return nil, fileInfo{}, ReadError(path, err)
	}
	if len(raw) > MaxReadBytes {
		return nil, fileInfo{}, &Error{Path: path, Reason: "text file exceeds read byte budget"}
	}
	if !utf8.Valid(raw) {
		return nil, fileInfo{}, &Error{Path: path, Reason: "invalid UTF-8 text"}
	}
	closed, err := f.Stat()
	if err != nil {
		return nil, fileInfo{}, ReadError(path, err)
	}
	after, err := os.Stat(path)
	if err != nil {
		return nil, fileInfo{}, ReadError(path, err)
	}
	if !os.SameFile(before, opened) || !os.SameFile(opened, closed) || !os.SameFile(closed, after) || infoOf(before) != infoOf(opened) || infoOf(opened) != infoOf(closed) || infoOf(closed) != infoOf(after) {
		return nil, fileInfo{}, &Error{Path: path, Reason: "file changed during read"}
	}
	return raw, infoOf(after), nil
}

func parseNote(r request) (*NoteRead, error) {
	raw, bodyInfo, err := stableRead(r.Path)
	if err != nil {
		return nil, err
	}
	body, legacy, hasLegacy := notedata.SplitLegacyFootmatter(string(raw))
	data, metaInfo, err := stableRead(r.Meta)
	found := err == nil
	if errors.Is(err, os.ErrNotExist) {
		if _, linkErr := os.Lstat(r.Meta); !os.IsNotExist(linkErr) {
			if linkErr == nil {
				linkErr = err
			}
			return nil, ReadError(r.Meta, linkErr)
		}
		err = nil
	}
	if err != nil {
		return nil, err
	}
	var meta notedata.Metadata
	if found {
		data, err = normalizeMetadata(data)
		if err != nil {
			return nil, &ParseFailure{r.Meta, err.Error()}
		}
		if err := yaml.Unmarshal(data, &meta); err != nil {
			return nil, &ParseFailure{r.Meta, "invalid note metadata"}
		}
		if meta.Version < 1 || meta.Version > notedata.MaxMetadataVersion {
			return nil, &ParseFailure{r.Meta, fmt.Sprintf("unsupported metadata version %d", meta.Version)}
		}
	} else if hasLegacy {
		data, err = normalizeMetadata([]byte(legacy))
		if err != nil {
			return nil, &ParseFailure{r.Path, err.Error()}
		}
		meta, err = notedata.ParseLegacyMetadata(string(data))
		if err != nil {
			return nil, &ParseFailure{r.Path, "invalid legacy metadata"}
		}
	}

	return &NoteRead{Body: body, Metadata: data, Found: found, Legacy: hasLegacy, BodyInfo: bodyInfo, MetaInfo: metaInfo, Tasks: task.Parse(body)}, nil
}

type childProcess struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	out   io.ReadCloser
	ended chan struct{}
}

func startProcess() (process, error) { return startMode(workerArg) }
func startMode(mode string) (process, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(executable, mode)
	cmd.Env = append(os.Environ(), "TRACK_INTERNAL_READ_WORKER=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	cmd.Stderr = &limitedDiagnostic{}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, err
	}
	p := &childProcess{cmd: cmd, in: stdin, out: stdout, ended: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(p.ended) }()
	return p, nil
}
func (p *childProcess) exchange(r request) (response, error) {
	var v response
	if err := frameWrite(p.in, r); err != nil {
		return v, err
	}
	err := frameRead(p.out, &v)
	return v, err
}
func (p *childProcess) stop()                 { _ = p.cmd.Process.Kill(); _ = p.in.Close(); _ = p.out.Close() }
func (p *childProcess) done() <-chan struct{} { return p.ended }

type limitedDiagnostic struct{ bytes.Buffer }

func (w *limitedDiagnostic) Write(b []byte) (int, error) {
	n := len(b)
	remaining := 4096 - w.Len()
	if remaining > 0 {
		w.Buffer.Write(b[:min(remaining, n)])
	}
	return n, nil
}
func ownerFor(path string) string {
	clean := filepath.Clean(path)
	for _, part := range []string{"note", "journal", ".track"} {
		marker := string(os.PathSeparator) + part
		if i := strings.Index(clean, marker+string(os.PathSeparator)); i >= 0 {
			return clean[:i]
		}
		if strings.HasSuffix(clean, marker) {
			return strings.TrimSuffix(clean, marker)
		}
	}
	return clean
}

func boundedMessage(message string) string {
	if len(message) > 1024 {
		return message[:1024] + "…"
	}
	return message
}

func normalizeMetadata(raw []byte) ([]byte, error) {
	if len(raw) > 4<<20 {
		return nil, errors.New("metadata exceeds byte budget")
	}
	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, errors.New("invalid note metadata")
	}
	nodes := 0
	var copyNode func(*yaml.Node, int) (*yaml.Node, error)
	copyNode = func(node *yaml.Node, depth int) (*yaml.Node, error) {
		nodes++
		if nodes > 100000 || depth > 64 || len(node.Value) > 1<<20 {
			return nil, errors.New("metadata structure exceeds read budget")
		}
		if node.Kind == yaml.AliasNode {
			if node.Alias == nil {
				return nil, errors.New("invalid metadata alias")
			}
			return copyNode(node.Alias, depth+1)
		}
		cloned := *node
		cloned.Anchor = ""
		cloned.Alias = nil
		cloned.Content = nil
		for _, child := range node.Content {
			next, err := copyNode(child, depth+1)
			if err != nil {
				return nil, err
			}
			cloned.Content = append(cloned.Content, next)
		}
		return &cloned, nil
	}
	normalized, err := copyNode(&document, 0)
	if err != nil {
		return nil, err
	}
	encoded, err := yaml.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	if len(encoded) > 4<<20 {
		return nil, errors.New("normalized metadata exceeds byte budget")
	}
	return encoded, nil
}
