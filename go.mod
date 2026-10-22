module go.etcd.io/bbolt

go 1.26.0

toolchain go1.26.10

require (
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
	github.com/stretchr/testify v1.11.1
	go.etcd.io/gofail v0.2.0
	golang.org/x/sync v0.20.0
	golang.org/x/sys v0.45.0
)

require (
	github.com/aclements/go-moremath v0.0.0-20210112150236-f10218a38794 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/mod v0.27.0 // indirect
	golang.org/x/perf v0.0.0-20250813145418-2f7363a06fe1 // indirect
	golang.org/x/tools v0.36.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

tool (
	go.etcd.io/gofail
	golang.org/x/perf/cmd/benchstat
	golang.org/x/tools/cmd/goimports
)
