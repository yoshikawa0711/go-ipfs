package dagcmd

import (
	"fmt"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/ipfs/go-ipfs/core/commands/cmdenv"
	"io/ioutil"
	"github.com/mattbaird/jsonpatch"
)

func dagDiff(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
	if len(req.Arguments) < 2 {
		return fmt.Errorf("less than 2 arguments provided for node comparison")
	}
	if len(req.Arguments) > 2 {
		return fmt.Errorf("more than 2 arguments provided for node comparison")
	}
	//nodeBefore := path.New(req.Arguments[0])
	//nodeAfter := path.New(req.Arguments[1])

	api, err := cmdenv.GetApi(env, req)
	if err != nil {
		return err
	}

	getArgNodeAsJson := func(argNumber int) (string, error) {
		r, err := getNodeWithCodec(req.Context, req.Arguments[argNumber], "dag-json", api)
		if err != nil {
			return "", err
		}

		jsonOutput, err := ioutil.ReadAll(r)
		if err != nil {
			return "", err
		}

		return string(jsonOutput), nil
	}

	nodeBefore, err := getArgNodeAsJson(0)
	if err != nil {
		return err
	}
	nodeAfter, err := getArgNodeAsJson(1)
	if err != nil {
		return err
	}

	fmt.Printf("BEFORE\n%s\n\n", string(nodeBefore))
	fmt.Printf("AFTER\n%s\n\n", string(nodeAfter))

	patch, err :=  jsonpatch.CreatePatch([]byte(nodeBefore), []byte(nodeAfter)) // FIXME: Remove string conversion above
	if err != nil {
		return err
	}

	fmt.Printf("======PATCH:======\n")

	for _, operation := range patch {
		fmt.Printf("DIFF:%s\n", operation.Json())
	}

	return nil
}
