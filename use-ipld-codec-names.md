# Use standard IPLD codec names across the CLI/HTTP API

Notes on issue https://github.com/ipfs/go-ipfs/issues/8471 being investigated.

Working on branch `schomatis/draft/ipld-codec-names` for both `go-ipfs` and its dependencies (like the interface one):
* <GH-LINK>
* <GH-LINK>

Per https://github.com/ipfs/go-ipfs/issues/8471#issuecomment-965523048: changing the core interface could be accepted.

My main point of comparison is between `ipfs dag put` and `ipfs block put`.

`dag put` resolves everything locally but `block put` goes through the `github.com/ipfs/interface-go-ipfs-core` which complicates things.

I'm not sure why `dag put` doesn't use that interface as well ([pending question](https://github.com/ipfs/go-ipfs/issues/8471#issuecomment-966356900)).

