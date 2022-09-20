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

	files "github.com/ipfs/go-ipfs-files"
	"github.com/ipfs/go-namesys/resolve"
	unixfile "github.com/ipfs/go-unixfs/file"
	"github.com/ipfs/kubo/tracing"
	"golang.org/x/image/draw"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-fetcher"
	ipld "github.com/ipfs/go-ipld-format"
	ipfspath "github.com/ipfs/go-path"
	ipfspathresolver "github.com/ipfs/go-path/resolver"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	path "github.com/ipfs/interface-go-ipfs-core/path"
)

// ResolveNode resolves the path `p` using Unixfs resolver, gets and returns the
// resolved Node.
func (api *CoreAPI) ResolveNode(ctx context.Context, p path.Path) (ipld.Node, error) {
	ctx, span := tracing.Span(ctx, "CoreAPI", "ResolveNode", trace.WithAttributes(attribute.String("path", p.String())))
	defer span.End()

	// split parameter
	p, param := separateParameter(p)

	rp, err := api.ResolvePath(ctx, p)
	if err != nil {
		return nil, err
	}

	// organize parameter and set cid.param
	c := rp.Cid()
	if param != "" {
		param, err = organizeParameter(param)
		if err != nil {
			return nil, err
		}

		c.SetParam(param)
	}

	// check and get resize cid <CID>&<Param>:<resized CID> table
	_, newc := c.IsExistResizeCid()

	var node ipld.Node
	if newc.Defined() {
		// if newc is not undef, get c and newc in parallel
		nodech, errch := getOriginalAndResizeInParallel(ctx, api, c, newc)
		errCount := 0

	nodeWait:
		for {
			select {
			case n := <-nodech:
				node = n
				break nodeWait
			case _ = <-errch:
				errCount += 1
				if errCount >= 2 {
					return nil, fmt.Errorf("searching node error: %s", c.StringWithParam())
				}

				continue
			case <-ctx.Done():
				return nil, fmt.Errorf("context timeout in searching node: %s", c.StringWithParam())
			}
		}
	} else if c.GetParam() != "" {
		// if c has parameter, get c with param and c without param in parallel
		nodech, errch := getNodeParallel(ctx, api, c)
		errCount := 0

	nodeWait2:
		for {
			select {
			case n := <-nodech:
				node = n
				break nodeWait2
			case _ = <-errch:
				errCount += 1
				if errCount >= 2 {
					return nil, fmt.Errorf("searching node error: %s", c.StringWithParam())
				}

				continue
			case <-ctx.Done():
				return nil, fmt.Errorf("context timeout in searching node: %s", c.StringWithParam())
			}
		}

	} else {
		// if newc is undef and c doesn't have parameter, simply get node
		node, err = getNode(ctx, api, c)
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

func separateParameter(p path.Path) (path.Path, string) {
	parts := strings.Split(p.String(), "&")
	if len(parts) >= 2 {
		return path.New(parts[0]), parts[1]
	} else {
		return path.New(parts[0]), ""
	}
}

func organizeParameter(param string) (string, error) {
	m, err := splitParameter(param)
	if err != nil {
		return "", err
	}

	var newparam string
	if v, ok := m["w"]; ok {
		newparam += "w=" + fmt.Sprint(v)
	}

	if v, ok := m["h"]; ok {
		if newparam != "" {
			newparam += ","
		}
		newparam += "h=" + fmt.Sprint(v)
	}

	if v, ok := m["a"]; ok {
		if newparam != "" {
			newparam += ","
		}
		newparam += "a=" + fmt.Sprint(v)
	}

	return newparam, nil
}

func splitParameter(params string) (map[string]int, error) {
	m := make(map[string]int)

	parts := strings.Split(params, ",")

	for _, v := range parts {
		param := strings.Split(v, "=")

		if len(param) != 2 {
			return nil, fmt.Errorf("invalid parameter: %s", v)
		}

		if param[0] == "w" || param[0] == "h" || param[0] == "a" {

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

func calculateNewParameter(m map[string]int, width int, height int) (map[string]int, error) {
	if v, ok := m["a"]; ok {
		if !(v == 0 || v == 1) {
			return m, fmt.Errorf("parameter a is equal 0 (spect fix) or 1 (not fix): v = %d", v)
		}

		// a=0 and set both of w and h
		newWidth, isSetW := m["w"]
		newHeight, isSetH := m["h"]
		if v == 0 && isSetW && isSetH {
			fmt.Println("you set aspect fix mode (a=0), and set w and h")
			fmt.Println("ipfs transform image using w only, and h determine from image aspect")

			newHeight = int((float64(height) / float64(width)) * float64(newWidth))
			m["h"] = newHeight

			fmt.Println("new parameter is w = " + fmt.Sprint(newWidth) + ", h = " + fmt.Sprint(newHeight))
			return m, nil
		}
	}

	return m, nil
}

func createNewImageNode(ctx context.Context, api *CoreAPI, node ipld.Node, param string) (ipld.Node, error) {
	m, err := splitParameter(param)
	if err != nil {
		return nil, err
	}

	ses := api.getSession(ctx)
	f, err := unixfile.NewUnixfsFile(ctx, ses.dag, node)
	if err != nil {
		return nil, err
	}

	fn, newparam, err := transformImage(f, m)
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

	fmt.Println("new cid is " + newnode.Cid().String())
	saveLink("/ipfs/"+node.Cid().String()+"&"+param, "/ipfs/"+newnode.Cid().String())
	saveLink("/ipfs/"+node.Cid().String()+"&"+newparam, "/ipfs/"+newnode.Cid().String())

	return newnode, nil
}

func transformImage(fn files.Node, m map[string]int) (files.Node, string, error) {
	f := files.ToFile(fn)

	w, err := os.Create("resource.png")
	_, err = io.Copy(w, f.(io.Reader))
	if err != nil {
		return nil, "", err
	}
	defer func() {
		w.Close()
		_ = os.Remove("resource.png")
	}()

	w, _ = os.Open("resource.png")
	img, _, err := image.Decode(w)
	if err != nil {
		return nil, "", err
	}

	width, height := img.Bounds().Dx(), img.Bounds().Dy()

	m, err = calculateNewParameter(m, width, height)
	if err != nil {
		return nil, "", err
	}

	newparam := ""

	if v, ok := m["w"]; ok {
		width = v
		newparam += "w=" + fmt.Sprint(v)
	}

	if v, ok := m["h"]; ok {
		height = v
		if newparam != "" {
			newparam += ","
		}
		newparam += "h=" + fmt.Sprint(v)
	}

	newimg := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.BiLinear.Scale(newimg, newimg.Bounds(), img, img.Bounds(), draw.Over, nil)

	output, err := os.Create("transformed.png")
	if err != nil {
		return nil, "", err
	}
	defer os.Remove("transformed.png")

	err = png.Encode(output, newimg)
	if err != nil {
		return nil, "", err
	}
	output.Close()

	output, _ = os.Open("transformed.png")
	stat, err := output.Stat()
	if err != nil {
		return nil, "", err
	}

	outnode, err := files.NewReaderPathFile("transformed.png", output, stat)
	if err != nil {
		return nil, "", err
	}

	return outnode, newparam, nil
}

func saveLink(oldpath, newpath string) error {
	linknode, err := os.OpenFile("linkstore", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0664)
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

	for _, v := range lines {
		if oldpath+":"+newpath == v {
			return nil
		}
	}

	_, err = linknode.WriteString(oldpath + ":" + newpath + "\n")
	if err != nil {
		return err
	}

	return nil
}

func getNode(ctx context.Context, api *CoreAPI, c cid.Cid) (ipld.Node, error) {
	node, err := api.dag.Get(ctx, c)
	if err != nil {
		return nil, err
	}

	return node, nil
}

func resizeNode(ctx context.Context, api *CoreAPI, node ipld.Node, param string) (ipld.Node, error) {
	rnode, err := createNewImageNode(ctx, api, node, param)
	if err != nil {
		return nil, err
	}

	return rnode, nil
}

// use each cid and get original and resized node in parallel
func getOriginalAndResizeInParallel(ctx context.Context, api *CoreAPI, original cid.Cid, resize cid.Cid) (chan ipld.Node, chan error) {
	nodech := make(chan ipld.Node)
	errch := make(chan error)

	go func() {
		param := original.GetParam()
		original.SetParam("")

		node, err := getNode(ctx, api, original)
		if err != nil {
			errch <- err
		} else {
			node, err = resizeNode(ctx, api, node, param)
			if err != nil {
				errch <- err
			} else {
				nodech <- node
			}
		}
	}()

	go func() {
		node, err := getNode(ctx, api, resize)
		if err != nil {
			errch <- err
		} else {
			nodech <- node
		}
	}()

	return nodech, errch
}

// get node in parallel
// one is search with parameter, other is without parameter
func getNodeParallel(ctx context.Context, api *CoreAPI, c cid.Cid) (chan ipld.Node, chan error) {
	nodech := make(chan ipld.Node)
	errch := make(chan error)

	go func() {
		original := c
		param := original.GetParam()
		original.SetParam("")

		node, err := getNode(ctx, api, original)
		if err != nil {
			errch <- err
		} else {
			rnode, err := resizeNode(ctx, api, node, param)
			if err != nil {
				errch <- err
			} else {
				nodech <- rnode
			}
		}
	}()

	go func() {
		node, err := getNode(ctx, api, c)
		if err != nil {
			errch <- err
		} else {
			nodech <- node
		}
	}()

	return nodech, errch
}
