package vaultfile

import (
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
)

func Stat(path string) (os.FileInfo, error) {
	v, err := bounded(request{Op: "stat", Path: path})
	return v.Info, err
}
func Lstat(path string) (os.FileInfo, error) {
	v, err := bounded(request{Op: "lstat", Path: path})
	return v.Info, err
}
func ReadDir(path string) ([]os.DirEntry, error) {
	v, err := bounded(request{Op: "directory", Path: path})
	if err != nil {
		return nil, err
	}
	out := make([]os.DirEntry, len(v.Entries))
	for i := range out {
		out[i] = v.Entries[i]
	}
	return out, nil
}
func EvalSymlinks(path string) (string, error) {
	v, err := bounded(request{Op: "canonical", Path: path})
	return v.Resolved, err
}
func ReadNote(ctx context.Context, path, meta string) (*NoteRead, error) {
	v, err := workers().call(ctx, request{Op: "note", Path: path, Meta: meta})
	if err != nil {
		return nil, &Error{Path: path, Reason: "note read unavailable: " + err.Error(), Err: err}
	}
	return v.Note, v.inputError(path)
}

func operationContext(ctx context.Context, r request) (response, error) {
	v, err := workers().call(ctx, r)
	if err != nil {
		return response{}, &Error{Path: r.Path, Reason: "read unavailable: " + err.Error(), Err: err}
	}
	if r.Op != "read" && r.Op != "note" && v.Errno != 0 {
		return v, &os.PathError{Op: r.Op, Path: r.Path, Err: syscall.Errno(v.Errno)}
	}
	return v, v.inputError(r.Path)
}
func ReadContext(ctx context.Context, path string) ([]byte, error) {
	v, err := operationContext(ctx, request{Op: "read", Path: path})
	return v.Data, err
}
func StatContext(ctx context.Context, path string) (os.FileInfo, error) {
	v, err := operationContext(ctx, request{Op: "stat", Path: path})
	return v.Info, err
}
func ReadDirContext(ctx context.Context, path string) ([]os.DirEntry, error) {
	v, err := operationContext(ctx, request{Op: "directory", Path: path})
	if err != nil {
		return nil, err
	}
	out := make([]os.DirEntry, len(v.Entries))
	for i := range out {
		out[i] = v.Entries[i]
	}
	return out, nil
}

func VerifyNote(ctx context.Context, path, meta string, staged *NoteRead) error {
	_, err := operationContext(ctx, request{Op: "verify", Path: path, Meta: meta, BodyVersion: staged.BodyInfo, MetaVersion: staged.MetaInfo, MetaFound: staged.Found})
	return err
}

func InputChanged(err error) bool {
	var input *Error
	return errors.As(err, &input) && strings.Contains(input.Reason, "changed")
}
func VersionOnly(read *NoteRead) *NoteRead {
	return &NoteRead{Found: read.Found, BodyInfo: read.BodyInfo, MetaInfo: read.MetaInfo}
}
