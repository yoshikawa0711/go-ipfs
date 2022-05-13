package coreapi

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	gopath "path"
	"strconv"
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
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	path "github.com/ipfs/interface-go-ipfs-core/path"

	"golang.org/x/image/draw"
)

// ResolveNode resolves the path `p` using Unixfs resolver, gets and returns the
// resolved Node.
func (api *CoreAPI) ResolveNode(ctx context.Context, p path.Path) (ipld.Node, error) {
	ctx, span := tracing.Span(ctx, "CoreAPI", "ResolveNode", trace.WithAttributes(attribute.String("path", p.String())))
	defer span.End()

	rp, err := api.ResolvePath(ctx, p)
	if err != nil {
		return nil, err
	}

	node, err := api.dag.Get(ctx, rp.Cid())
	if err != nil {
		return nil, err
	}
	return node, nil
}

// ResolvePath resolves the path `p` using Unixfs resolver, returns the
// resolved path.
func (api *CoreAPI) ResolvePath(ctx context.Context, p path.Path) (path.Resolved, error) {
	ctx, span := tracing.Span(ctx, "CoreAPI", "ResolvePath", trace.WithAttributes(attribute.String("path", p.String())))
	defer span.End()

	var newp path.Path
	ok, pstr, err := hasParameter(p.String())
	if err != nil {
		return nil, err
	}

	if ok {
		if ok, pstr = isExitLink(pstr); ok {
			newp = path.New(pstr)
		} else {
			var params string
			p, params = separateParameter(pstr)
			fn, err := api.Unixfs().Get(ctx, p)
			if err != nil {
				return nil, err
			}
			defer fn.(io.Closer).Close()

			m, err := parameterSplit(params)
			if err != nil {
				return nil, err
			}

			newfn, err := transformImage(fn, m)
			if err != nil {
				return nil, err
			}
			defer newfn.(io.Closer).Close()

			newp, err = api.Unixfs().Add(ctx, newfn)
			if err != nil {
				return nil, err
			}

			fmt.Println("new image cid: " + newp.String())

			linknode, err := os.OpenFile("linkstore", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0664)
			if err != nil {
				return nil, err
			}
			defer linknode.Close()

			_, err = linknode.WriteString(p.String() + "&" + params + ":" + newp.String() + "\n")
			if err != nil {
				return nil, err
			}
		}

		p = newp

	}

	if _, ok := p.(path.Resolved); ok {
		return p.(path.Resolved), nil
	}
	if err := p.IsValid(); err != nil {
		return nil, err
	}

	ipath := ipfspath.Path(p.String())
	ipath, err = resolve.ResolveIPNS(ctx, api.namesys, ipath)
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

func isExitLink(pstr string) (bool, string) {
	linkstore, err := os.Open("linkstore")
	if os.IsNotExist(err) {
		return false, pstr
	}
	defer linkstore.Close()

	listfile, err := os.ReadFile("linkstore")
	if err != nil {
		return false, pstr
	}

	list := string(listfile)
	lines := strings.Split(list, "\n")

	for _, v := range lines {
		pathlist := strings.Split(v, ":")

		parts := strings.Split(pstr, "&")
		searchpath, err := ipfspath.ParsePath(parts[0])
		if err != nil {
			return false, pstr
		}

		if searchpath.String()+"&"+parts[1] == pathlist[0] {
			return true, pathlist[1]
		}
	}

	return false, pstr
}

func hasParameter(pstr string) (bool, string, error) {
	parts := strings.Split(pstr, "&")

	if len(parts) == 1 {
		return false, pstr, nil
	}

	if len(parts) > 2 {
		return false, pstr, fmt.Errorf("invalid parameter: %s", pstr)
	}

	m, err := parameterSplit(parts[1])
	if err != nil {
		return false, pstr, err
	}

	var newparams string
	if v, ok := m["w"]; ok {
		newparams += "w=" + fmt.Sprint(v)
	}

	if v, ok := m["h"]; ok {
		if newparams != "" {
			newparams += ","
		}
		newparams += "h=" + fmt.Sprint(v)
	}

	fmt.Println(parts[0] + "&" + newparams)

	return true, parts[0] + "&" + newparams, nil
}

// Before executing saparateParameter,
// you need to execute hasParameter and
// check return is true
func separateParameter(txt string) (path.Path, string) {
	parts := strings.Split(txt, "&")

	return path.New(parts[0]), parts[1]
}

func parameterSplit(params string) (map[string]int, error) {
	m := make(map[string]int)

	parts := strings.Split(params, ",")

	for _, v := range parts {
		param := strings.Split(v, "=")

		if len(param) != 2 {
			return nil, fmt.Errorf("invalid parameter: %s", v)
		}

		if param[0] == "w" || param[0] == "h" {

			if _, ok := m[param[0]]; !ok {
				var err error

				m[param[0]], err = strconv.Atoi(param[1])
				if err != nil {
					return nil, err
				}

			} else {
				return nil, fmt.Errorf("invalid parameter: %s: already exist", param[0])
			}

		} else {
			return nil, fmt.Errorf("invalid parameter: %s is not supported", param[0])
		}
	}

	return m, nil

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
