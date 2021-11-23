# Use standard IPLD codec names across the CLI/HTTP API

Notes on issue https://github.com/ipfs/go-ipfs/issues/8471 being investigated.

Working on branch `schomatis/draft/ipld-codec-names` for both `go-ipfs` and its dependencies (like the interface one):
* <GH-LINK>
* <GH-LINK>

Per https://github.com/ipfs/go-ipfs/issues/8471#issuecomment-965523048: changing the core interface could be accepted.

My main point of comparison is between `ipfs dag put` and `ipfs block put`.

`dag put` resolves everything locally but `block put` goes through the `github.com/ipfs/interface-go-ipfs-core` which complicates things.

I'm not sure why `dag put` doesn't use that interface as well ([pending question](https://github.com/ipfs/go-ipfs/issues/8471#issuecomment-966356900)).

Per https://github.com/ipfs/go-ipfs/issues/8471#issuecomment-972378534:
* We dropped support for CID v0 for `dag put`.
* We want to still support it for `block put`.

We want to be able to split/filter multicodec tags as discussed in https://github.com/multiformats/go-multicodec/issues/58.

First stab at adding a store codec in block put: https://github.com/ipfs/interface-go-ipfs-core/pull/80

## ipfs cid codecs

From issue:

> ipfs cid codecs should contain a --supported flag that lists which codecs are known to go-ipfs per Enumerate available encodings for dag get #8171 (review). This has some tradeoffs if different commands support different subsets of codecs, but for now this seems reasonable.

> To support ipfs cid codecs --supported we can for now leverage the ipld-prime global codec registry https://github.com/ipld/go-ipld-prime/blob/b9a89e847312334af141788b303fd78b4f076fe3/multicodec/defaultRegistry.go#L23


> To support ipfs cid codecs we need a list of all the IPLD codecs. This list can either be manually generated (as it is in go-cid), or a likely better approach is to modify https://github.com/multiformats/go-multicodec to allow retrieving codes by type (e.g. the IPLD type). WDYT @mvdan ?

> This could then be leveraged inside of go-cid to generate the list of CIDs on init by depending on go-multicodec



