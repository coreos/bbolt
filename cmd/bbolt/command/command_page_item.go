package command

import (
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"go.etcd.io/bbolt/internal/common"
	"go.etcd.io/bbolt/internal/guts_cli"
)

type pageItemOptions struct {
	keyOnly   bool
	valueOnly bool
	format    string
}

func newPageItemCommand() *cobra.Command {
	var opt pageItemOptions
	pageItemCmd := &cobra.Command{
		Use:   "page-item [options] <bbolt-file> pageid itemid",
		Short: "print a page item key and value in a bbolt database",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath := args[0]
			pageID, err := strconv.ParseUint(args[1], 10, 64)
			if err != nil {
				return err
			}
			itemID, err := strconv.ParseUint(args[2], 10, 64)
			if err != nil {
				return err
			}
			return pageItemFunc(cmd, opt, dbPath, pageID, itemID)
		},
	}
	opt.AddFlags(pageItemCmd.Flags())

	return pageItemCmd
}

func (o *pageItemOptions) AddFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&o.keyOnly, "key-only", false, "Print only the key")
	fs.BoolVar(&o.valueOnly, "value-only", false, "Print only the value")
	fs.StringVar(&o.format, "format", "auto", "Output format one of: "+FORMAT_MODES)
}

func pageItemFunc(cmd *cobra.Command, cfg pageItemOptions, dbPath string, pageID, itemID uint64) (err error) {
	if cfg.keyOnly && cfg.valueOnly {
		return errors.New("the --key-only or --value-only flag may be set, but not both")
	}

	if _, err := checkSourceDBPath(dbPath); err != nil {
		return err
	}

	// retrieve page info and page size.
	_, buf, err := guts_cli.ReadPage(dbPath, pageID)
	if err != nil {
		return err
	}

	if !cfg.valueOnly {
		err := pageItemPrintLeafItemKey(cmd.OutOrStdout(), buf, itemID, cfg.format)
		if err != nil {
			return err
		}
	}
	if !cfg.keyOnly {
		err := pageItemPrintLeafItemValue(cmd.OutOrStdout(), buf, itemID, cfg.format)
		if err != nil {
			return err
		}
	}

	return
}

func pageItemPrintLeafItemKey(w io.Writer, pageBytes []byte, index uint64, format string) error {
	k, _, err := pageItemLeafPageElement(pageBytes, index)
	if err != nil {
		return err
	}

	return writelnBytes(w, k, format)
}

func pageItemPrintLeafItemValue(w io.Writer, pageBytes []byte, index uint64, format string) error {
	_, v, err := pageItemLeafPageElement(pageBytes, index)
	if err != nil {
		return err
	}
	return writelnBytes(w, v, format)
}

func pageItemLeafPageElement(pageBytes []byte, index uint64) ([]byte, []byte, error) {
	p := common.LoadPage(pageBytes)
	if index >= uint64(p.Count()) {
		return nil, nil, fmt.Errorf("leafPageElement: expected item index less than %d, but got %d", p.Count(), index)
	}
	if p.Typ() != "leaf" {
		return nil, nil, fmt.Errorf("leafPageElement: expected page type of 'leaf', but got '%s'", p.Typ())
	}

	e := p.LeafPageElement(uint16(index))
	return e.Key(), e.Value(), nil
}
