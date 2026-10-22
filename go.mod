module go.etcd.io/bbolt

go 1.26.0

toolchain go1.26.10

require (
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
	github.com/stretchr/testify v1.12.1
	go.etcd.io/gofail v0.2.0
	golang.org/x/sync v0.24.0
	golang.org/x/sys v0.49.0
)

require (
	github.com/aclements/go-moremath v0.0.0-20210112150236-f10218a38794 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/mod v0.27.0 // indirect
	golang.org/x/perf v0.0.0-20250813145418-2f7363a06fe1 // indirect
	golang.org/x/tools v0.36.0 // indirect
)

tool (
	go.etcd.io/gofail
	golang.org/x/perf/cmd/benchstat
	golang.org/x/tools/cmd/goimports
)
