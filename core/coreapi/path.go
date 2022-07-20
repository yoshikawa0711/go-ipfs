package coreapi

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	gopath "path"
	"strings"

	"github.com/ipfs/go-namesys/resolve"
	"github.com/ipfs/kubo/tracing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-fetcher"
	files "github.com/ipfs/go-ipfs-files"
	ipld "github.com/ipfs/go-ipld-format"
	ipfspath "github.com/ipfs/go-path"
	ipfspathresolver "github.com/ipfs/go-path/resolver"
	unixfile "github.com/ipfs/go-unixfs/file"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	path "github.com/ipfs/interface-go-ipfs-core/path"

	"golang.org/x/image/draw"
)

// ResolveNode resolves the path `p` using Unixfs resolver, gets and returns the
// resolved Node.
func (api *CoreAPI) ResolveNode(ctx context.Context, p path.Path) (ipld.Node, error) {
	ctx, span := tracing.Span(ctx, "CoreAPI", "ResolveNode", trace.WithAttributes(attribute.String("path", p.String())))
	defer span.End()

	pstr, param := cid.SeparateParameter(p.String())
	if param != "" {
		newparam, err := cid.OrganizeParameter(param)
		if err != nil {
			return nil, err
		}
		param = newparam
	}
	p = path.New(pstr)

	rp, err := api.ResolvePath(ctx, p)
	if err != nil {
		return nil, err
	}
	c := rp.Cid()
	if param != "" {
		c.SetParam(param)
		if ok, newcid := c.IsExistResizeCid(); ok {
			newcid.SetRequest(c.StringWithParam())
			c = newcid
			fmt.Println("New Cid is " + c.String())
		} else {
			c.SetRequest(c.StringWithParam())
		}

		c.SetParam("")
	}

	node, err := api.dag.Get(ctx, c)
	if err != nil {
		if ipld.IsNotFound(err) && c.GetRequest() != "" {
			origincid, err := c.GetRequestCid()
			if err != nil {
				return nil, err
			}

			originparam := origincid.GetParam()
			origincid.SetParam("")

			originnode, err := api.ResolveNode(ctx, path.IpfsPath(origincid))
			if err != nil {
				return nil, err
			}

			node, err = createNewImageNode(ctx, api, originnode, originparam)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	reqcid, err := node.Cid().GetRequestCid()
	if err == nil && node.Cid().String() == reqcid.String() {
		node, err = createNewImageNode(ctx, api, node, reqcid.GetParam())
		if err != nil {
			return nil, err
		}
	}

	return node, nil
}

// ResolvePath resolves the path `p` using Unixfs resolver, returns the
// resolved path.
func (api *CoreAPI) ResolvePath(ctx context.Context, p path.Path) (path.Resolved, error) {
	ctx, span := tracing.Span(ctx, "CoreAPI", "ResolvePath", trace.WithAttributes(attribute.String("path", p.String())))
	defer span.End()

	if _, ok := p.(path.Resolved); ok {
		return p.(path.Resolved), nil
	}
	if err := p.IsValid(); err != nil {
		return nil, err
	}

	ipath := ipfspath.Path(p.String())
	ipath, err := resolve.ResolveIPNS(ctx, api.namesys, ipath)
	if err == resolve.ErrNoNamesys {
		return nil, coreiface.ErrOffline
	} else if err != nil {
		return nil, err
	}

	if ipath.Segments()[0] != "ipfs" && ipath.Segments()[0] != "ipld" {
		return nil, fmt.Errorf("unsupported path namespace: %s", p.Namespace())
	}

	var dataFetcher fetcher.Factory
	if ipath.Segments()[0] == "ipld" {
		dataFetcher = api.ipldFetcherFactory
	} else {
		dataFetcher = api.unixFSFetcherFactory
	}
	resolver := ipfspathresolver.NewBasicResolver(dataFetcher)

	node, rest, err := resolver.ResolveToLastNode(ctx, ipath)
	if err != nil {
		return nil, err
	}

	root, err := cid.Parse(ipath.Segments()[1])
	if err != nil {
		return nil, err
	}

	return path.NewResolvedPath(ipath, node, root, gopath.Join(rest...)), nil
}

func transformImage(fn files.Node, m map[string]int) (files.Node, error) {
	f := files.ToFile(fn)

	w, err := os.Create("resource.png")
	_, err = io.Copy(w, f.(io.Reader))
	if err != nil {
		return nil, err
	}
	defer func() {
		w.Close()
		_ = os.Remove("resource.png")
	}()

	w, _ = os.Open("resource.png")
	img, _, err := image.Decode(w)
	if err != nil {
		return nil, err
	}

	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	if v, ok := m["w"]; ok {
		width = v
	}

	if v, ok := m["h"]; ok {
		height = v
	}

	newimg := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.BiLinear.Scale(newimg, newimg.Bounds(), img, img.Bounds(), draw.Over, nil)

	output, err := os.Create("transformed.png")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = os.Remove("transformed.png")
	}()

	err = png.Encode(output, newimg)
	if err != nil {
		return nil, err
	}
	output.Close()

	output, _ = os.Open("transformed.png")
	stat, err := output.Stat()
	if err != nil {
		return nil, err
	}

	outnode, err := files.NewReaderPathFile("transformed.png", output, stat)
	if err != nil {
		return nil, err
	}

	return outnode, nil
}

func createNewImageNode(ctx context.Context, api *CoreAPI, node ipld.Node, param string) (ipld.Node, error) {
	m, err := cid.SplitParameter(param)
	if err != nil {
		return nil, err
	}

	ses := api.getSession(ctx)
	f, err := unixfile.NewUnixfsFile(ctx, ses.dag, node)
	if err != nil {
		return nil, err
	}

	fn, err := transformImage(f, m)
	if err != nil {
		return nil, err
	}

	rp, err := api.Unixfs().Add(ctx, fn)
	if err != nil {
		return nil, err
	}

	newnode, err := api.dag.Get(ctx, rp.Cid())
	if err != nil {
		return nil, err
	}

	return newnode, nil
}

func saveLink(oldpath, newpath string) error {
	linknode, err := os.OpenFile("linkstore", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0664)
	if err != nil {
		return err
	}
	defer linknode.Close()

	_, err = linknode.WriteString(oldpath + ":" + newpath + "\n")
	if err != nil {
		return err
	}

	return nil
}

func changeLink(oldpath, newpath string) error {
	linknode, err := os.OpenFile("linkstore", os.O_RDWR, 0664)
	if err != nil {
		return err
	}
	defer linknode.Close()

	listfile, err := os.ReadFile("linkstore")
	if err != nil {
		return err
	}

	list := string(listfile)
	lines := strings.Split(list, "\n")

	var count int64 = 0

	for _, v := range lines {
		pathlist := strings.Split(v, ":")

		if pathlist[0] == oldpath {
			pathlist[1] = newpath

			_, err = linknode.WriteAt([]byte(pathlist[0]+":"+pathlist[1]+"\n"), count)
			if err != nil {
				return err
			}
		}

		count += int64(len([]byte(v + "\n")))
	}

	return nil

}
