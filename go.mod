module m31labs.dev/graft

go 1.25.0

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/klauspost/compress v1.18.4
	github.com/odvcencio/arbiter v1.6.0
	github.com/odvcencio/gotreesitter v0.19.1
	github.com/spf13/cobra v1.10.2
	golang.org/x/crypto v0.46.0
	m31labs.dev/canopy v0.15.0
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/sys v0.41.0 // indirect
)

replace m31labs.dev/canopy => ../canopy
