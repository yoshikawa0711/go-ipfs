package dagcmd

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ipfs/go-ipfs/core/commands/cmdenv"
	"github.com/ipfs/interface-go-ipfs-core/path"

	fetcher "github.com/ipfs/go-fetcher"
	cmds "github.com/ipfs/go-ipfs-cmds"
	ipld "github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/codec/dagjson"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/polydawn/refmt/json"
)

func dagGet(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) (err error) {
	api, err := cmdenv.GetApi(env, req)
	if err != nil {
		return
	}

	ctx := req.Context

	rp, err := api.ResolvePath(ctx, path.New(req.Arguments[0]))
	if err != nil {
		return
	}

	f := api.Node().NewSession(ctx)
	var n ipld.Node
	n, err = fetcher.Block(ctx, f, cidlink.Link{Cid: rp.Cid()})
	if err != nil {
		return
	}

	defer func() {
		if r := recover(); r == "unreachable" {
			// dagjson.Marshal panics when it encounters a dagpb.Link, so fall back to getting a format node.
			obj, e := api.Dag().Get(ctx, rp.Cid())
			if e != nil {
				err = e
				return
			}
			var out interface{} = obj
			if len(rp.Remainder()) > 0 {
				rem := strings.Split(rp.Remainder(), "/")
				out, _, err = obj.Resolve(rem)
				if err != nil {
					return
				}
			}
			err = cmds.EmitOnce(res, &out)
		}
	}()

	buf := new(bytes.Buffer)

	// Encode the node as a json string
	err = dagjson.Marshal(n, json.NewEncoder(buf, json.EncodeOptions{}), true)
	if err != nil {
		err = fmt.Errorf("encoding error: %s", err)
		return
	}

	if n.Kind() != ipld.Kind_Map {
		err = fmt.Errorf("expected map type for encoded node")
		return
	}

	m := make(map[string]interface{})
	err = json.NewUnmarshaller(buf).Unmarshal(&m)
	if err != nil {
		return
	}

	var out interface{} = m
	/*
		if len(rp.Remainder()) > 0 {
			rem := strings.Split(rp.Remainder(), "/")
			final, _, err := obj.Resolve(rem)
			if err != nil {
				return err
			}
			out = final
		}
	*/
	err = cmds.EmitOnce(res, &out)
	return
}
