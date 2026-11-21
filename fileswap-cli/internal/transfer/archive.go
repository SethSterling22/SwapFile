package transfer

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// Several files or a folder travel as one ZIP that the sender builds while
// sending (docs/protocol.md §4): nothing is staged on disk, and the
// receiver gets a single .zip. Already-compressed formats are stored as
// they are; everything else is deflated.

// Entry is a file to put in an archive.
type Entry struct {
	Path    string // on disk
	Name    string // inside the ZIP, slash-separated
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
}

// ErrNotRegular is returned for a path that is neither a regular file nor a folder.
var ErrNotRegular = errors.New("not a regular file or folder")

// CollectEntries lists the files to archive. A file goes at the top of the
// ZIP; a folder keeps its name and structure. Symlinks and special files
// inside folders are skipped, so nothing outside the chosen paths is read.
func CollectEntries(paths []string) ([]Entry, error) {
	var entries []Entry
	used := map[string]bool{}
	unique := func(name string) string {
		if !used[name] {
			used[name] = true
			return name
		}
		ext := splitExt(name)
		base := strings.TrimSuffix(name, ext)
		for i := 1; ; i++ {
			if c := fmt.Sprintf("%s (%d)%s", base, i, ext); !used[c] {
				used[c] = true
				return c
			}
		}
	}
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		switch {
		case st.Mode().IsRegular():
			entries = append(entries, Entry{Path: p, Name: unique(SafeName(st.Name())), Size: st.Size(), Mode: st.Mode(), ModTime: st.ModTime()})
		case st.IsDir():
			root := unique(SafeName(filepath.Base(filepath.Clean(p))))
			err := filepath.WalkDir(p, func(fp string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.Type().IsRegular() {
					return nil // folders are implied by the file names; skip links and devices
				}
				info, err := d.Info()
				if err != nil {
					return err
				}
				rel, err := filepath.Rel(p, fp)
				if err != nil {
					return err
				}
				entries = append(entries, Entry{Path: fp, Name: path.Join(root, filepath.ToSlash(rel)), Size: info.Size(), Mode: info.Mode(), ModTime: info.ModTime()})
				return nil
			})
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("%s: %w", p, ErrNotRegular)
		}
	}
	return entries, nil
}

// ArchiveName names the ZIP: "<folder>.zip" for a single folder, else
// "fileswap-<date>-<time>.zip".
func ArchiveName(paths []string, now time.Time) string {
	if len(paths) == 1 {
		if st, err := os.Stat(paths[0]); err == nil && st.IsDir() {
			return SafeName(filepath.Base(filepath.Clean(paths[0]))) + ".zip"
		}
	}
	return now.Format("fileswap-2006-01-02-150405.zip")
}

// TotalSize is the sum of the entries' sizes.
func TotalSize(entries []Entry) int64 {
	var n int64
	for _, e := range entries {
		n += e.Size
	}
	return n
}

// storedExt lists formats that are already compressed: deflating them
// costs time and saves nothing, so they are stored.
var storedExt = map[string]bool{}

func init() {
	for _, e := range strings.Fields(`
		.jpg .jpeg .png .gif .webp .heic .heif .avif .jxl
		.mp4 .m4v .mov .mkv .webm .avi .wmv .mpg .mpeg
		.mp3 .m4a .aac .ogg .oga .opus .flac .wma
		.zip .gz .tgz .bz2 .xz .zst .7z .rar .lz .lz4 .br
		.jar .apk .ipa .deb .rpm .pkg .dmg .iso .msi .whl .war
		.docx .xlsx .pptx .odt .ods .odp .epub .pdf .woff .woff2`) {
		storedExt[e] = true
	}
}

// Compressible reports whether a file is worth deflating.
func Compressible(name string) bool {
	return !storedExt[strings.ToLower(path.Ext(name))]
}

// WriteZip streams the entries as a ZIP into w (ZIP64 is used
// automatically past 4 GB). read is increased by the bytes read from the
// files, for progress. A file that changes size while being read fails the
// archive rather than producing a silently wrong entry.
func WriteZip(ctx context.Context, w io.Writer, entries []Entry, read *atomic.Int64) error {
	zw := zip.NewWriter(w)
	buf := make([]byte, 256<<10)
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		h := &zip.FileHeader{Name: e.Name, Method: zip.Store, Modified: e.ModTime}
		if Compressible(e.Name) {
			h.Method = zip.Deflate
		}
		h.SetMode(e.Mode.Perm())
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		f, err := os.Open(e.Path)
		if err != nil {
			return err
		}
		n, err := io.CopyBuffer(fw, &countingReader{r: f, n: read, ctx: ctx}, buf)
		f.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", e.Path, err)
		}
		if n != e.Size {
			return fmt.Errorf("%s: %w", e.Path, &Error{Code: ErrSizeMismatch, Detail: "file changed while sending"})
		}
	}
	return zw.Close()
}

type countingReader struct {
	r   io.Reader
	n   *atomic.Int64
	ctx context.Context
}

func (c *countingReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	if c.n != nil {
		c.n.Add(int64(n))
	}
	return n, err
}
