package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	gopath "path"
	"slices"
	"strconv"
	"strings"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/ipfs/kubo/config"
	"github.com/ipfs/kubo/core"
	"github.com/ipfs/kubo/core/commands/cmdenv"

	bservice "github.com/ipfs/boxo/blockservice"
	offline "github.com/ipfs/boxo/exchange/offline"
	dag "github.com/ipfs/boxo/ipld/merkledag"
	ft "github.com/ipfs/boxo/ipld/unixfs"
	mfs "github.com/ipfs/boxo/mfs"
	"github.com/ipfs/boxo/path"
	cid "github.com/ipfs/go-cid"
	cidenc "github.com/ipfs/go-cidutil/cidenc"
	cmds "github.com/ipfs/go-ipfs-cmds"
	ipld "github.com/ipfs/go-ipld-format"
	logging "github.com/ipfs/go-log/v2"
	iface "github.com/ipfs/kubo/core/coreiface"
	mh "github.com/multiformats/go-multihash"
)

var flog = logging.Logger("cmds/files")

// FilesCmd is the 'ipfs files' command
var FilesCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Interact with unixfs files.",
		ShortDescription: `
Files is an API for manipulating IPFS objects as if they were a Unix
filesystem.

The files facility interacts with MFS (Mutable File System). MFS acts as a
single, dynamic filesystem mount. MFS has a root CID that is transparently
updated when a change happens (and can be checked with "ipfs files stat /").

All files and folders within MFS are respected and will not be deleted
during garbage collections. However, a DAG may be referenced in MFS without
being fully available locally (MFS content is lazy loaded when accessed).
MFS is independent from the list of pinned items ("ipfs pin ls"). Calls to
"ipfs pin add" and "ipfs pin rm" will add and remove pins independently of
MFS. If MFS content that was additionally pinned is removed by calling
"ipfs files rm", it will still remain pinned.

Content added with "ipfs add" (which by default also becomes pinned), is not
added to MFS. Any content can be lazily referenced from MFS with the command
"ipfs files cp /ipfs/<cid> /some/path/" (see ipfs files cp --help).

NOTE: Most of the subcommands of 'ipfs files' accept the '--flush' flag. It
defaults to true and ensures two things: 1) that the changes are reflected in
the full MFS structure (updated CIDs) 2) that the parent-folder's cache is
cleared. Use caution when setting this flag to false. It will improve
performance for large numbers of file operations, but it does so at the cost
of consistency guarantees and unbound growth of the directories' in-memory
caches.  If the daemon is unexpectedly killed before running 'ipfs files
flush' on the files in question, then data may be lost. This also applies to
run 'ipfs repo gc' concurrently with '--flush=false' operations. We recommend
flushing paths regularly with 'ipfs files flush', specially the folders on
which many write operations are happening, as a way to clear the directory
cache, free memory and speed up read operations.`,
	},
	Options: []cmds.Option{
		cmds.BoolOption(filesFlushOptionName, "f", "Flush target and ancestors after write.").WithDefault(true),
	},
	Subcommands: map[string]*cmds.Command{
		"read":  filesReadCmd,
		"write": filesWriteCmd,
		"mv":    filesMvCmd,
		"cp":    filesCpCmd,
		"ls":    filesLsCmd,
		"mkdir": filesMkdirCmd,
		"stat":  filesStatCmd,
		"rm":    filesRmCmd,
		"flush": filesFlushCmd,
		"chcid": filesChcidCmd,
		"chmod": filesChmodCmd,
		"touch": filesTouchCmd,
	},
}

const (
	filesCidVersionOptionName = "cid-version"
	filesHashOptionName       = "hash"
)

var (
	cidVersionOption = cmds.IntOption(filesCidVersionOptionName, "cid-ver", "Cid version to use. (experimental)")
	hashOption       = cmds.StringOption(filesHashOptionName, "Hash function to use. Will set Cid version to 1 if used. (experimental)")
)

var errFormat = errors.New("format was set by multiple options. Only one format option is allowed")

type statOutput struct {
	Hash           string
	Size           uint64
	CumulativeSize uint64
	Blocks         int
	Type           string
	WithLocality   bool   `json:",omitempty"`
	Local          bool   `json:",omitempty"`
	SizeLocal      uint64 `json:",omitempty"`
	Mode           uint32 `json:",omitempty"`
	Mtime          int64  `json:",omitempty"`
	MtimeNsecs     int    `json:",omitempty"`
}

func (s *statOutput) MarshalJSON() ([]byte, error) {
	type so statOutput
	out := &struct {
		*so
		Mode string `json:",omitempty"`
	}{so: (*so)(s)}

	if s.Mode != 0 {
		out.Mode = fmt.Sprintf("%04o", s.Mode)
	}
	return json.Marshal(out)
}

func (s *statOutput) UnmarshalJSON(data []byte) error {
	var err error
	type so statOutput
	tmp := &struct {
		*so
		Mode string `json:",omitempty"`
	}{so: (*so)(s)}

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	if tmp.Mode != "" {
		mode, err := strconv.ParseUint(tmp.Mode, 8, 32)
		if err == nil {
			s.Mode = uint32(mode)
		}
	}
	return err
}

const (
	defaultStatFormat = `<hash>
Size: <size>
CumulativeSize: <cumulsize>
ChildBlocks: <childs>
Type: <type>
Mode: <mode> (<mode-octal>)
Mtime: <mtime>`
	filesFormatOptionName    = "format"
	filesSizeOptionName      = "size"
	filesWithLocalOptionName = "with-local"
	filesStatUnspecified     = "not set"
)

var filesStatCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Display file status.",
	},

	Arguments: []cmds.Argument{
		cmds.StringArg("path", true, false, "Path to node to stat."),
	},
	Options: []cmds.Option{
		cmds.StringOption(filesFormatOptionName, "Print statistics in given format. Allowed tokens: "+
			"<hash> <size> <cumulsize> <type> <childs> and optional <mode> <mode-octal> <mtime> <mtime-secs> <mtime-nsecs>."+
			"Conflicts with other format options.").WithDefault(defaultStatFormat),
		cmds.BoolOption(filesHashOptionName, "Print only hash. Implies '--format=<hash>'. Conflicts with other format options."),
		cmds.BoolOption(filesSizeOptionName, "Print only size. Implies '--format=<cumulsize>'. Conflicts with other format options."),
		cmds.BoolOption(filesWithLocalOptionName, "Compute the amount of the dag that is local, and if possible the total size"),
	},
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		_, err := statGetFormatOptions(req)
		if err != nil {
			return cmds.Errorf(cmds.ErrClient, "invalid parameters: %s", err)
		}

		node, err := cmdenv.GetNode(env)
		if err != nil {
			return err
		}

		api, err := cmdenv.GetApi(env, req)
		if err != nil {
			return err
		}

		path, err := checkPath(req.Arguments[0])
		if err != nil {
			return err
		}

		withLocal, _ := req.Options[filesWithLocalOptionName].(bool)

		enc, err := cmdenv.GetCidEncoder(req)
		if err != nil {
			return err
		}

		var dagserv ipld.DAGService
		if withLocal {
			// an offline DAGService will not fetch from the network
			dagserv = dag.NewDAGService(bservice.New(
				node.Blockstore,
				offline.Exchange(node.Blockstore),
			))
		} else {
			dagserv = node.DAG
		}

		nd, err := getNodeFromPath(req.Context, node, api, path)
		if err != nil {
			return err
		}

		o, err := statNode(nd, enc)
		if err != nil {
			return err
		}

		if !withLocal {
			return cmds.EmitOnce(res, o)
		}

		local, sizeLocal, err := walkBlock(req.Context, dagserv, nd)
		if err != nil {
			return err
		}

		o.WithLocality = true
		o.Local = local
		o.SizeLocal = sizeLocal

		return cmds.EmitOnce(res, o)
	},
	Encoders: cmds.EncoderMap{
		cmds.Text: cmds.MakeTypedEncoder(func(req *cmds.Request, w io.Writer, out *statOutput) error {
			mode, modeo := filesStatUnspecified, filesStatUnspecified
			if out.Mode != 0 {
				mode = strings.ToLower(os.FileMode(out.Mode).String())
				modeo = "0" + strconv.FormatInt(int64(out.Mode&0x1FF), 8)
			}
			mtime, mtimes, mtimens := filesStatUnspecified, filesStatUnspecified, filesStatUnspecified
			if out.Mtime > 0 {
				mtime = time.Unix(out.Mtime, int64(out.MtimeNsecs)).UTC().Format("2 Jan 2006, 15:04:05 MST")
				mtimes = strconv.FormatInt(out.Mtime, 10)
				mtimens = strconv.Itoa(out.MtimeNsecs)
			}

			s, _ := statGetFormatOptions(req)
			s = strings.Replace(s, "<hash>", out.Hash, -1)
			s = strings.Replace(s, "<size>", fmt.Sprintf("%d", out.Size), -1)
			s = strings.Replace(s, "<cumulsize>", fmt.Sprintf("%d", out.CumulativeSize), -1)
			s = strings.Replace(s, "<childs>", fmt.Sprintf("%d", out.Blocks), -1)
			s = strings.Replace(s, "<type>", out.Type, -1)
			s = strings.Replace(s, "<mode>", mode, -1)
			s = strings.Replace(s, "<mode-octal>", modeo, -1)
			s = strings.Replace(s, "<mtime>", mtime, -1)
			s = strings.Replace(s, "<mtime-secs>", mtimes, -1)
			s = strings.Replace(s, "<mtime-nsecs>", mtimens, -1)

			fmt.Fprintln(w, s)

			if out.WithLocality {
				fmt.Fprintf(w, "Local: %s of %s (%.2f%%)\n",
					humanize.Bytes(out.SizeLocal),
					humanize.Bytes(out.CumulativeSize),
					100.0*float64(out.SizeLocal)/float64(out.CumulativeSize),
				)
			}

			return nil
		}),
	},
	Type: statOutput{},
}

func moreThanOne(a, b, c bool) bool {
	return a && b || b && c || a && c
}

func statGetFormatOptions(req *cmds.Request) (string, error) {
	hash, _ := req.Options[filesHashOptionName].(bool)
	size, _ := req.Options[filesSizeOptionName].(bool)
	format, _ := req.Options[filesFormatOptionName].(string)

	if moreThanOne(hash, size, format != defaultStatFormat) {
		return "", errFormat
	}

	if hash {
		return "<hash>", nil
	} else if size {
		return "<cumulsize>", nil
	} else {
		return format, nil
	}
}

func statNode(nd ipld.Node, enc cidenc.Encoder) (*statOutput, error) {
	c := nd.Cid()

	cumulsize, err := nd.Size()
	if err != nil {
		return nil, err
	}

	switch n := nd.(type) {
	case *dag.ProtoNode:
		return statProtoNode(n, enc, c, cumulsize)
	case *dag.RawNode:
		return &statOutput{
			Hash:           enc.Encode(c),
			Blocks:         0,
			Size:           cumulsize,
			CumulativeSize: cumulsize,
			Type:           "file",
		}, nil
	default:
		return nil, errors.New("not unixfs node (proto or raw)")
	}
}

func statProtoNode(n *dag.ProtoNode, enc cidenc.Encoder, cid cid.Cid, cumulsize uint64) (*statOutput, error) {
	d, err := ft.FSNodeFromBytes(n.Data())
	if err != nil {
		return nil, err
	}

	stat := statOutput{
		Hash:           enc.Encode(cid),
		Blocks:         len(n.Links()),
		Size:           d.FileSize(),
		CumulativeSize: cumulsize,
	}

	switch d.Type() {
	case ft.TDirectory, ft.THAMTShard:
		stat.Type = "directory"
	case ft.TFile, ft.TSymlink, ft.TMetadata, ft.TRaw:
		stat.Type = "file"
	default:
		return nil, fmt.Errorf("unrecognized node type: %s", d.Type())
	}

	if mode := d.Mode(); mode != 0 {
		stat.Mode = uint32(mode)
	} else if d.Type() == ft.TSymlink {
		stat.Mode = uint32(os.ModeSymlink | 0x1FF)
	}

	if mt := d.ModTime(); !mt.IsZero() {
		stat.Mtime = mt.Unix()
		if ns := mt.Nanosecond(); ns > 0 {
			stat.MtimeNsecs = ns
		}
	}

	return &stat, nil
}

func walkBlock(ctx context.Context, dagserv ipld.DAGService, nd ipld.Node) (bool, uint64, error) {
	// Start with the block data size
	sizeLocal := uint64(len(nd.RawData()))

	local := true

	for _, link := range nd.Links() {
		child, err := dagserv.Get(ctx, link.Cid)

		if ipld.IsNotFound(err) {
			local = false
			continue
		}

		if err != nil {
			return local, sizeLocal, err
		}

		childLocal, childLocalSize, err := walkBlock(ctx, dagserv, child)
		if err != nil {
			return local, sizeLocal, err
		}

		// Recursively add the child size
		local = local && childLocal
		sizeLocal += childLocalSize
	}

	return local, sizeLocal, nil
}

var errFilesCpInvalidUnixFS = errors.New("cp: source must be a valid UnixFS (dag-pb or raw codec)")
var filesCpCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Add references to IPFS files and directories in MFS (or copy within MFS).",
		ShortDescription: `
"ipfs files cp" can be used to add references to any IPFS file or directory
(usually in the form /ipfs/<CID>, but also any resolvable path) into MFS.
This performs a lazy copy: the full DAG will not be fetched, only the root
node being copied.

It can also be used to copy files within MFS, but in the case when an
IPFS-path matches an existing MFS path, the IPFS path wins.

In order to add content to MFS from disk, you can use "ipfs add" to obtain the
IPFS Content Identifier and then "ipfs files cp" to copy it into MFS:

$ ipfs add --quieter --pin=false <your file>
# ...
# ... outputs the root CID at the end
$ ipfs files cp /ipfs/<CID> /your/desired/mfs/path

If you wish to fully copy content from a different IPFS peer into MFS, do not
forget to force IPFS to fetch the full DAG after doing a "cp" operation. i.e:

$ ipfs files cp /ipfs/<CID> /your/desired/mfs/path
$ ipfs pin add <CID>

The lazy-copy feature can also be used to protect partial DAG contents from
garbage collection. i.e. adding the Wikipedia root to MFS would not download
all the Wikipedia, but will prevent any downloaded Wikipedia-DAG content from
being GC'ed.
`,
	},
	Arguments: []cmds.Argument{
		cmds.StringArg("source", true, false, "Source IPFS or MFS path to copy."),
		cmds.StringArg("dest", true, false, "Destination within MFS."),
	},
	Options: []cmds.Option{
		cmds.BoolOption(forceOptionName, "Force overwrite of existing files."),
		cmds.BoolOption(filesParentsOptionName, "p", "Make parent directories as needed."),
	},
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		nd, err := cmdenv.GetNode(env)
		if err != nil {
			return err
		}

		prefix, err := getPrefixNew(req)
		if err != nil {
			return err
		}

		api, err := cmdenv.GetApi(env, req)
		if err != nil {
			return err
		}

		src, err := checkPath(req.Arguments[0])
		if err != nil {
			return err
		}
		src = strings.TrimRight(src, "/")

		dst, err := checkPath(req.Arguments[1])
		if err != nil {
			return err
		}

		if dst[len(dst)-1] == '/' {
			dst += gopath.Base(src)
		}

		node, err := getNodeFromPath(req.Context, nd, api, src)
		if err != nil {
			return fmt.Errorf("cp: cannot get node from path %s: %s", src, err)
		}

		// Sanity-check: ensure root CID is a valid UnixFS (dag-pb or raw block)
		// Context: https://github.com/ipfs/kubo/issues/10331
		srcCidType := node.Cid().Type()
		switch srcCidType {
		case cid.Raw:
			if _, ok := node.(*dag.RawNode); !ok {
				return errFilesCpInvalidUnixFS
			}
		case cid.DagProtobuf:
			if _, ok := node.(*dag.ProtoNode); !ok {
				return errFilesCpInvalidUnixFS
			}
			if _, err = ft.FSNodeFromBytes(node.(*dag.ProtoNode).Data()); err != nil {
				return fmt.Errorf("%w: %v", errFilesCpInvalidUnixFS, err)
			}
		default:
			return errFilesCpInvalidUnixFS
		}

		mkParents, _ := req.Options[filesParentsOptionName].(bool)
		if mkParents {
			err := ensureContainingDirectoryExists(nd.FilesRoot, dst, prefix)
			if err != nil {
				return err
			}
		}

		force, _ := req.Options[forceOptionName].(bool)
		if force {
			if err = unlinkNodeIfExists(nd, dst); err != nil {
				return fmt.Errorf("cp: cannot unlink existing file: %s", err)
			}
		}

		err = mfs.PutNode(nd.FilesRoot, dst, node)
		if err != nil {
			return fmt.Errorf("cp: cannot put node in path %s: %s", dst, err)
		}

		flush, _ := req.Options[filesFlushOptionName].(bool)
		if flush {
			if _, err := mfs.FlushPath(req.Context, nd.FilesRoot, dst); err != nil {
				return fmt.Errorf("cp: cannot flush the created file %s: %s", dst, err)
			}
			// Flush parent to clear directory cache and free memory.
			parent := gopath.Dir(dst)
			if _, err = mfs.FlushPath(req.Context, nd.FilesRoot, parent); err != nil {
				return fmt.Errorf("cp: cannot flush the created file's parent folder %s: %s", dst, err)
			}
		}

		return nil
	},
}

func getNodeFromPath(ctx context.Context, node *core.IpfsNode, api iface.CoreAPI, p string) (ipld.Node, error) {
	switch {
	case strings.HasPrefix(p, "/ipfs/"):
		pth, err := path.NewPath(p)
		if err != nil {
			return nil, err
		}

		return api.ResolveNode(ctx, pth)
	default:
		fsn, err := mfs.Lookup(node.FilesRoot, p)
		if err != nil {
			return nil, err
		}

		return fsn.GetNode()
	}
}

func unlinkNodeIfExists(node *core.IpfsNode, path string) error {
	dir, name := gopath.Split(path)
	parent, err := mfs.Lookup(node.FilesRoot, dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	pdir, ok := parent.(*mfs.Directory)
	if !ok {
		return fmt.Errorf("not a directory: %s", dir)
	}

	// Attempt to unlink if child is a file, ignore error since
	// we are only concerned with unlinking an existing file.
	child, err := pdir.Child(name)
	if err != nil {
		return nil // no child file, nothing to unlink
	}

	if child.Type() != mfs.TFile {
		return fmt.Errorf("not a file: %s", path)
	}

	return pdir.Unlink(name)
}

type filesLsOutput struct {
	Entries []mfs.NodeListing
}

const (
	longOptionName     = "long"
	dontSortOptionName = "U"
)

var filesLsCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "List directories in the local mutable namespace.",
		ShortDescription: `
List directories in the local mutable namespace (works on both IPFS and MFS paths).

Examples:

    $ ipfs files ls /welcome/docs/
    about
    contact
    help
    quick-start
    readme
    security-notes

    $ ipfs files ls /myfiles/a/b/c/d
    foo
    bar
`,
	},
	Arguments: []cmds.Argument{
		cmds.StringArg("path", false, false, "Path to show listing for. Defaults to '/'."),
	},
	Options: []cmds.Option{
		cmds.BoolOption(longOptionName, "l", "Use long listing format."),
		cmds.BoolOption(dontSortOptionName, "Do not sort; list entries in directory order."),
	},
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		var arg string

		if len(req.Arguments) == 0 {
			arg = "/"
		} else {
			arg = req.Arguments[0]
		}

		path, err := checkPath(arg)
		if err != nil {
			return err
		}

		nd, err := cmdenv.GetNode(env)
		if err != nil {
			return err
		}

		fsn, err := mfs.Lookup(nd.FilesRoot, path)
		if err != nil {
			return err
		}

		long, _ := req.Options[longOptionName].(bool)

		enc, err := cmdenv.GetCidEncoder(req)
		if err != nil {
			return err
		}

		switch fsn := fsn.(type) {
		case *mfs.Directory:
			if !long {
				var output []mfs.NodeListing
				names, err := fsn.ListNames(req.Context)
				if err != nil {
					return err
				}

				for _, name := range names {
					output = append(output, mfs.NodeListing{
						Name: name,
					})
				}
				return cmds.EmitOnce(res, &filesLsOutput{output})
			}
			listing, err := fsn.List(req.Context)
			if err != nil {
				return err
			}
			return cmds.EmitOnce(res, &filesLsOutput{listing})
		case *mfs.File:
			_, name := gopath.Split(path)
			out := &filesLsOutput{[]mfs.NodeListing{{Name: name}}}
			if long {
				out.Entries[0].Type = int(fsn.Type())

				size, err := fsn.Size()
				if err != nil {
					return err
				}
				out.Entries[0].Size = size

				nd, err := fsn.GetNode()
				if err != nil {
					return err
				}
				out.Entries[0].Hash = enc.Encode(nd.Cid())
			}
			return cmds.EmitOnce(res, out)
		default:
			return errors.New("unrecognized type")
		}
	},
	Encoders: cmds.EncoderMap{
		cmds.Text: cmds.MakeTypedEncoder(func(req *cmds.Request, w io.Writer, out *filesLsOutput) error {
			noSort, _ := req.Options[dontSortOptionName].(bool)
			if !noSort {
				slices.SortFunc(out.Entries, func(a, b mfs.NodeListing) int {
					return strings.Compare(a.Name, b.Name)
				})
			}

			long, _ := req.Options[longOptionName].(bool)
			for _, o := range out.Entries {
				if long {
					if o.Type == int(mfs.TDir) {
						o.Name += "/"
					}
					fmt.Fprintf(w, "%s\t%s\t%d\n", o.Name, o.Hash, o.Size)
				} else {
					fmt.Fprintf(w, "%s\n", o.Name)
				}
			}

			return nil
		}),
	},
	Type: filesLsOutput{},
}

const (
	filesOffsetOptionName = "offset"
	filesCountOptionName  = "count"
)

var filesReadCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Read a file from MFS.",
		ShortDescription: `
Read a specified number of bytes from a file at a given offset. By default,
it will read the entire file similar to the Unix cat.

Examples:

    $ ipfs files read /test/hello
    hello
	`,
	},

	Arguments: []cmds.Argument{
		cmds.StringArg("path", true, false, "Path to file to be read."),
	},
	Options: []cmds.Option{
		cmds.Int64Option(filesOffsetOptionName, "o", "Byte offset to begin reading from."),
		cmds.Int64Option(filesCountOptionName, "n", "Maximum number of bytes to read."),
	},
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		nd, err := cmdenv.GetNode(env)
		if err != nil {
			return err
		}

		path, err := checkPath(req.Arguments[0])
		if err != nil {
			return err
		}

		fsn, err := mfs.Lookup(nd.FilesRoot, path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}

		fi, ok := fsn.(*mfs.File)
		if !ok {
			return fmt.Errorf("%s was not a file", path)
		}

		rfd, err := fi.Open(mfs.Flags{Read: true})
		if err != nil {
			return err
		}

		defer rfd.Close()

		offset, _ := req.Options[offsetOptionName].(int64)
		if offset < 0 {
			return fmt.Errorf("cannot specify negative offset")
		}

		filen, err := rfd.Size()
		if err != nil {
			return err
		}

		if int64(offset) > filen {
			return fmt.Errorf("offset was past end of file (%d > %d)", offset, filen)
		}

		_, err = rfd.Seek(int64(offset), io.SeekStart)
		if err != nil {
			return err
		}

		var r io.Reader = &contextReaderWrapper{R: rfd, ctx: req.Context}
		count, found := req.Options[filesCountOptionName].(int64)
		if found {
			if count < 0 {
				return fmt.Errorf("cannot specify negative 'count'")
			}
			r = io.LimitReader(r, int64(count))
		}
		return res.Emit(r)
	},
}

type contextReader interface {
	CtxReadFull(context.Context, []byte) (int, error)
}

type contextReaderWrapper struct {
	R   contextReader
	ctx context.Context
}

func (crw *contextReaderWrapper) Read(b []byte) (int, error) {
	return crw.R.CtxReadFull(crw.ctx, b)
}

var filesMvCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Move files.",
		ShortDescription: `
Move files around. Just like the traditional Unix mv.

Example:

    $ ipfs files mv /myfs/a/b/c /myfs/foo/newc

`,
	},

	Arguments: []cmds.Argument{
		cmds.StringArg("source", true, false, "Source file to move."),
		cmds.StringArg("dest", true, false, "Destination path for file to be moved to."),
	},
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		nd, err := cmdenv.GetNode(env)
		if err != nil {
			return err
		}

		flush, _ := req.Options[filesFlushOptionName].(bool)

		src, err := checkPath(req.Arguments[0])
		if err != nil {
			return err
		}
		dst, err := checkPath(req.Arguments[1])
		if err != nil {
			return err
		}

		err = mfs.Mv(nd.FilesRoot, src, dst)
		if err != nil {
			return err
		}
		if flush {
			parentSrc := gopath.Dir(src)
			parentDst := gopath.Dir(dst)
			// Flush parent to clear directory cache and free memory.
			if _, err = mfs.FlushPath(req.Context, nd.FilesRoot, parentDst); err != nil {
				return fmt.Errorf("cp: cannot flush the destination file's parent folder %s: %s", dst, err)
			}

			// Avoid re-flushing when moving within the same folder.
			if parentSrc != parentDst {
				if _, err = mfs.FlushPath(req.Context, nd.FilesRoot, parentSrc); err != nil {
					return fmt.Errorf("cp: cannot flush the source's file's parent folder %s: %s", dst, err)
				}
			}

			if _, err = mfs.FlushPath(req.Context, nd.FilesRoot, "/"); err != nil {
				return err
			}
		}

		return nil
	},
}

const (
	filesCreateOptionName    = "create"
	filesParentsOptionName   = "parents"
	filesTruncateOptionName  = "truncate"
	filesRawLeavesOptionName = "raw-leaves"
	filesFlushOptionName     = "flush"
)

var filesWriteCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Append to (modify) a file in MFS.",
		ShortDescription: `
A low-level MFS command that allows you to append data to a file. If you want
to add a file without modifying an existing one, use 'ipfs add --to-files'
instead.
`,
		LongDescription: `
A low-level MFS command that allows you to append data at the end of a file, or
specify a beginning offset within a file to write to. The entire length of the
input will be written.

If the '--create' option is specified, the file will be created if it does not
exist. Nonexistent intermediate directories will not be created unless the
'--parents' option is specified.

Newly created files will have the same CID version and hash function of the
parent directory unless the '--cid-version' and '--hash' options are used.

Newly created leaves will be in the legacy format (Protobuf) if the
CID version is 0, or raw if the CID version is non-zero.  Use of the
'--raw-leaves' option will override this behavior.

If the '--flush' option is set to false, changes will not be propagated to the
merkledag root. This can make operations much faster when doing a large number
of writes to a deeper directory structure.

EXAMPLE:

    echo "hello world" | ipfs files write --create --parents /myfs/a/b/file
    echo "hello world" | ipfs files write --truncate /myfs/a/b/file

WARNING:

Usage of the '--flush=false' option does not guarantee data durability until
the tree has been flushed. This can be accomplished by running 'ipfs files
stat' on the file or any of its ancestors.

WARNING:

The CID produced by 'files write' will be different from 'ipfs add' because
'ipfs file write' creates a trickle-dag optimized for append-only operations
See '--trickle' in 'ipfs add --help' for more information.

If you want to add a file without modifying an existing one,
use 'ipfs add' with '--to-files':

  > ipfs files mkdir -p /myfs/dir
  > ipfs add example.jpg --to-files /myfs/dir/
  > ipfs files ls /myfs/dir/
  example.jpg

See '--to-files' in 'ipfs add --help' for more information.
`,
	},
	Arguments: []cmds.Argument{
		cmds.StringArg("path", true, false, "Path to write to."),
		cmds.FileArg("data", true, false, "Data to write.").EnableStdin(),
	},
	Options: []cmds.Option{
		cmds.Int64Option(filesOffsetOptionName, "o", "Byte offset to begin writing at."),
		cmds.BoolOption(filesCreateOptionName, "e", "Create the file if it does not exist."),
		cmds.BoolOption(filesParentsOptionName, "p", "Make parent directories as needed."),
		cmds.BoolOption(filesTruncateOptionName, "t", "Truncate the file to size zero before writing."),
		cmds.Int64Option(filesCountOptionName, "n", "Maximum number of bytes to read."),
		cmds.BoolOption(filesRawLeavesOptionName, "Use raw blocks for newly created leaf nodes. (experimental)"),
		cidVersionOption,
		hashOption,
	},
	Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) (retErr error) {
		path, err := checkPath(req.Arguments[0])
		if err != nil {
			return err
		}

		nd, err := cmdenv.GetNode(env)
		if err != nil {
			return err
		}

		cfg, err := nd.Repo.Config()
		if err != nil {
			return err
		}

		create, _ := req.Options[filesCreateOptionName].(bool)
		mkParents, _ := req.Options[filesParentsOptionName].(bool)
		trunc, _ := req.Options[filesTruncateOptionName].(bool)
		flush, _ := req.Options[filesFlushOptionName].(bool)
		rawLeaves, rawLeavesDef := req.Options[filesRawLeavesOptionName].(bool)

		if !rawLeavesDef && cfg.Import.UnixFSRawLeaves != config.Default {
			rawLeavesDef = true
			rawLeaves = cfg.Import.UnixFSRawLeaves.WithDefault(config.DefaultUnixFSRawLeaves)
		}

		prefix, err := getPrefixNew(req)
		if err != nil {
			return err
		}

		offset, _ := req.Options[filesOffsetOptionName].(int64)
		if offset < 0 {
			return fmt.Errorf("cannot have negative write offset")
		}

		if mkParents {
			err := ensureContainingDirectoryExists(nd.FilesRoot, path, prefix)
			if err != nil {
				return err
			}
		}

		fi, err := getFileHandle(nd.FilesRoot, path, create, prefix)
		if err != nil {
			return err
		}
		if rawLeavesDef {
			fi.RawLeaves = rawLeaves
		}

		wfd, err := fi.Open(mfs.Flags{Write: true, Sync: flush})
		if err != nil {
			return err
		}

		defer func() {
			err := wfd.Close()
			if err != nil {
				if retErr == nil {
					retErr = err
				} else {
					flog.Error("files: error closing file mfs file descriptor", err)
				}
			}
			if flush {
				// Flush parent to clear directory cache and free memory.
				parent := gopath.Dir(path)
				if _, err := mfs.FlushPath(req.Context, nd.FilesRoot, parent); err != nil {
					if retErr == nil {
						retErr = err
					} else {
						flog.Error("files: flushing the parent folder", err)
					}
				}
			}
		}()

		if trunc {
			if err := wfd.Truncate(0); err != nil {
				return err
			}
		}

		count, countfound := req.Options[filesCountOptionName].(int64)
		if countfound && count < 0 {
			return fmt.Errorf("cannot have negative byte count")
		}

		_, err = wfd.Seek(int64(offset), io.SeekStart)
		if err != nil {
			flog.Error("seekfail: ", err)
			return err
		}

		var r io.Reader
		r, err = cmdenv.GetFileArg(req.Files.Entries())
		if err != nil {
			return err
		}
		if countfound {
			r = io.LimitReader(r, int64(count))
		}

		_, err = io.Copy(wfd, r)
		return err
	},
}

var filesMkdirCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Make directories.",
		ShortDescription: `
Create the directory if it does not already exist.

The directory will have the same CID version and hash function of the
parent directory unless the --cid-version and --hash options are used.

NOTE: All paths must be absolute.

Examples:

    $ ipfs files mkdir /test/newdir
    $ ipfs files mkdir -p /test/does/not/exist/yet
`,
	},

	Arguments: []cmds.Argument{
		cmds.StringArg("path", true, false, "Path to dir to make."),
	},
	Options: []cmds.Option{
		cmds.BoolOption(filesParentsOptionName, "p", "No error if existing, make parent directories as needed."),
		cidVersionOption,
		hashOption,
	},
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		n, err := cmdenv.GetNode(env)
		if err != nil {
			return err
		}

		dashp, _ := req.Options[filesParentsOptionName].(bool)
		dirtomake, err := checkPath(req.Arguments[0])
		if err != nil {
			return err
		}

		flush, _ := req.Options[filesFlushOptionName].(bool)

		prefix, err := getPrefix(req)
		if err != nil {
			return err
		}
		root := n.FilesRoot

		err = mfs.Mkdir(root, dirtomake, mfs.MkdirOpts{
			Mkparents:  dashp,
			Flush:      flush,
			CidBuilder: prefix,
		})

		return err
	},
}

type flushRes struct {
	Cid string
}

var filesFlushCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Flush a given path's data to disk.",
		ShortDescription: `
Flush a given path to the disk. This is only useful when other commands
are run with the '--flush=false'.
`,
	},
	Arguments: []cmds.Argument{
		cmds.StringArg("path", false, false, "Path to flush. Default: '/'."),
	},
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		nd, err := cmdenv.GetNode(env)
		if err != nil {
			return err
		}

		enc, err := cmdenv.GetCidEncoder(req)
		if err != nil {
			return err
		}

		path := "/"
		if len(req.Arguments) > 0 {
			path = req.Arguments[0]
		}

		n, err := mfs.FlushPath(req.Context, nd.FilesRoot, path)
		if err != nil {
			return err
		}

		return cmds.EmitOnce(res, &flushRes{enc.Encode(n.Cid())})
	},
	Type: flushRes{},
}

var filesChcidCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Change the CID version or hash function of the root node of a given path.",
		ShortDescription: `
Change the CID version or hash function of the root node of a given path.
`,
	},
	Arguments: []cmds.Argument{
		cmds.StringArg("path", false, false, "Path to change. Default: '/'."),
	},
	Options: []cmds.Option{
		cidVersionOption,
		hashOption,
	},
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		nd, err := cmdenv.GetNode(env)
		if err != nil {
			return err
		}

		path := "/"
		if len(req.Arguments) > 0 {
			path = req.Arguments[0]
		}

		flush, _ := req.Options[filesFlushOptionName].(bool)

		prefix, err := getPrefix(req)
		if err != nil {
			return err
		}

		if err := updatePath(nd.FilesRoot, path, prefix); err != nil {
			return err
		}
		if flush {
			if _, err = mfs.FlushPath(req.Context, nd.FilesRoot, path); err != nil {
				return err
			}
			// Flush parent to clear directory cache and free memory.
			parent := gopath.Dir(path)
			if _, err = mfs.FlushPath(req.Context, nd.FilesRoot, parent); err != nil {
				return err
			}
		}
		return nil
	},
}

func updatePath(rt *mfs.Root, pth string, builder cid.Builder) error {
	if builder == nil {
		return nil
	}

	nd, err := mfs.Lookup(rt, pth)
	if err != nil {
		return err
	}

	switch n := nd.(type) {
	case *mfs.Directory:
		n.SetCidBuilder(builder)
	default:
		return fmt.Errorf("can only update directories")
	}

	return nil
}

var filesRmCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Remove a file from MFS.",
		ShortDescription: `
Remove files or directories.

    $ ipfs files rm /foo
    $ ipfs files ls /bar
    cat
    dog
    fish
    $ ipfs files rm -r /bar
`,
	},

	Arguments: []cmds.Argument{
		cmds.StringArg("path", true, true, "File to remove."),
	},
	Options: []cmds.Option{
		cmds.BoolOption(recursiveOptionName, "r", "Recursively remove directories."),
		cmds.BoolOption(forceOptionName, "Forcibly remove target at path; implies -r for directories"),
	},
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		nd, err := cmdenv.GetNode(env)
		if err != nil {
			return err
		}
		// if '--force' specified, it will remove anything else,
		// including file, directory, corrupted node, etc
		force, _ := req.Options[forceOptionName].(bool)
		dashr, _ := req.Options[recursiveOptionName].(bool)
		var errs []error
		for _, arg := range req.Arguments {
			path, err := checkPath(arg)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s is not a valid path: %w", arg, err))
				continue
			}

			if err := removePath(nd.FilesRoot, path, force, dashr); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
			}
		}
		if len(errs) > 0 {
			for _, err = range errs {
				e := res.Emit(err.Error())
				if e != nil {
					return e
				}
			}
			return fmt.Errorf("can't remove some files")
		}
		return nil
	},
}

func removePath(filesRoot *mfs.Root, path string, force bool, dashr bool) error {
	if path == "/" {
		return fmt.Errorf("cannot delete root")
	}

	// 'rm a/b/c/' will fail unless we trim the slash at the end
	if path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}

	dir, name := gopath.Split(path)

	pdir, err := getParentDir(filesRoot, dir)
	if err != nil {
		if force && err == os.ErrNotExist {
			return nil
		}
		return err
	}

	if force {
		err := pdir.Unlink(name)
		if err != nil {
			if err == os.ErrNotExist {
				return nil
			}
			return err
		}
		return pdir.Flush()
	}

	// get child node by name, when the node is corrupted and nonexistent,
	// it will return specific error.
	child, err := pdir.Child(name)
	if err != nil {
		return err
	}

	switch child.(type) {
	case *mfs.Directory:
		if !dashr {
			return fmt.Errorf("path is a directory, use -r to remove directories")
		}
	}

	err = pdir.Unlink(name)
	if err != nil {
		return err
	}

	return pdir.Flush()
}

func getPrefixNew(req *cmds.Request) (cid.Builder, error) {
	cidVer, cidVerSet := req.Options[filesCidVersionOptionName].(int)
	hashFunStr, hashFunSet := req.Options[filesHashOptionName].(string)

	if !cidVerSet && !hashFunSet {
		return nil, nil
	}

	if hashFunSet && cidVer == 0 {
		cidVer = 1
	}

	prefix, err := dag.PrefixForCidVersion(cidVer)
	if err != nil {
		return nil, err
	}

	if hashFunSet {
		hashFunCode, ok := mh.Names[strings.ToLower(hashFunStr)]
		if !ok {
			return nil, fmt.Errorf("unrecognized hash function: %s", strings.ToLower(hashFunStr))
		}
		prefix.MhType = hashFunCode
		prefix.MhLength = -1
	}

	return &prefix, nil
}

func getPrefix(req *cmds.Request) (cid.Builder, error) {
	cidVer, cidVerSet := req.Options[filesCidVersionOptionName].(int)
	hashFunStr, hashFunSet := req.Options[filesHashOptionName].(string)

	if !cidVerSet && !hashFunSet {
		return nil, nil
	}

	if hashFunSet && cidVer == 0 {
		cidVer = 1
	}

	prefix, err := dag.PrefixForCidVersion(cidVer)
	if err != nil {
		return nil, err
	}

	if hashFunSet {
		hashFunCode, ok := mh.Names[strings.ToLower(hashFunStr)]
		if !ok {
			return nil, fmt.Errorf("unrecognized hash function: %s", strings.ToLower(hashFunStr))
		}
		prefix.MhType = hashFunCode
		prefix.MhLength = -1
	}

	return &prefix, nil
}

func ensureContainingDirectoryExists(r *mfs.Root, path string, builder cid.Builder) error {
	dirtomake := gopath.Dir(path)

	if dirtomake == "/" {
		return nil
	}

	return mfs.Mkdir(r, dirtomake, mfs.MkdirOpts{
		Mkparents:  true,
		CidBuilder: builder,
	})
}

func getFileHandle(r *mfs.Root, path string, create bool, builder cid.Builder) (*mfs.File, error) {
	target, err := mfs.Lookup(r, path)
	switch err {
	case nil:
		fi, ok := target.(*mfs.File)
		if !ok {
			return nil, fmt.Errorf("%s was not a file", path)
		}
		return fi, nil

	case os.ErrNotExist:
		if !create {
			return nil, err
		}

		// if create is specified and the file doesn't exist, we create the file
		dirname, fname := gopath.Split(path)
		pdir, err := getParentDir(r, dirname)
		if err != nil {
			return nil, err
		}

		if builder == nil {
			builder = pdir.GetCidBuilder()
		}

		nd := dag.NodeWithData(ft.FilePBData(nil, 0))
		err = nd.SetCidBuilder(builder)
		if err != nil {
			return nil, err
		}
		err = pdir.AddChild(fname, nd)
		if err != nil {
			return nil, err
		}

		fsn, err := pdir.Child(fname)
		if err != nil {
			return nil, err
		}

		fi, ok := fsn.(*mfs.File)
		if !ok {
			return nil, errors.New("expected *mfs.File, didn't get it. This is likely a race condition")
		}
		return fi, nil

	default:
		return nil, err
	}
}

func checkPath(p string) (string, error) {
	if len(p) == 0 {
		return "", fmt.Errorf("paths must not be empty")
	}

	if p[0] != '/' {
		return "", fmt.Errorf("paths must start with a leading slash")
	}

	cleaned := gopath.Clean(p)
	if p[len(p)-1] == '/' && p != "/" {
		cleaned += "/"
	}
	return cleaned, nil
}

func getParentDir(root *mfs.Root, dir string) (*mfs.Directory, error) {
	parent, err := mfs.Lookup(root, dir)
	if err != nil {
		return nil, err
	}

	pdir, ok := parent.(*mfs.Directory)
	if !ok {
		return nil, errors.New("expected *mfs.Directory, didn't get it. This is likely a race condition")
	}
	return pdir, nil
}

var filesChmodCmd = &cmds.Command{
	Status: cmds.Experimental,
	Helptext: cmds.HelpText{
		Tagline: "Change optional POSIX mode permissions",
		ShortDescription: `
The mode argument must be specified in Unix numeric notation.

    $ ipfs files chmod 0644 /foo
    $ ipfs files stat /foo
    ...
    Type: file
    Mode: -rw-r--r-- (0644)
    ...
`,
	},
	Arguments: []cmds.Argument{
		cmds.StringArg("mode", true, false, "Mode to apply to node (numeric notation)"),
		cmds.StringArg("path", true, false, "Path to apply mode"),
	},
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		nd, err := cmdenv.GetNode(env)
		if err != nil {
			return err
		}

		path, err := checkPath(req.Arguments[1])
		if err != nil {
			return err
		}

		mode, err := strconv.ParseInt(req.Arguments[0], 8, 32)
		if err != nil {
			return err
		}

		return mfs.Chmod(nd.FilesRoot, path, os.FileMode(mode))
	},
}

var filesTouchCmd = &cmds.Command{
	Status: cmds.Experimental,
	Helptext: cmds.HelpText{
		Tagline: "Set or change optional POSIX modification times.",
		ShortDescription: `
Examples:
    # set modification time to now.
    $ ipfs files touch /foo
    # set a custom modification time.
    $ ipfs files touch --mtime=1630937926 /foo
`,
	},
	Arguments: []cmds.Argument{
		cmds.StringArg("path", true, false, "Path of target to update."),
	},
	Options: []cmds.Option{
		cmds.Int64Option(mtimeOptionName, "Modification time in seconds before or since the Unix Epoch to apply to created UnixFS entries."),
		cmds.UintOption(mtimeNsecsOptionName, "Modification time fraction in nanoseconds"),
	},
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		nd, err := cmdenv.GetNode(env)
		if err != nil {
			return err
		}

		path, err := checkPath(req.Arguments[0])
		if err != nil {
			return err
		}

		mtime, _ := req.Options[mtimeOptionName].(int64)
		nsecs, _ := req.Options[mtimeNsecsOptionName].(uint)

		var ts time.Time
		if mtime != 0 {
			ts = time.Unix(mtime, int64(nsecs)).UTC()
		} else {
			ts = time.Now().UTC()
		}

		return mfs.Touch(nd.FilesRoot, path, ts)
	},
}
