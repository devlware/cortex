package gcp

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"

	"cloud.google.com/go/bigtable"
	"google.golang.org/grpc/status"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/pkg/errors"
)

type tableClient struct {
	cfg    Config
	client *bigtable.AdminClient

	tableInfo       map[string]*bigtable.TableInfo
	tableExpiration time.Time
}

// NewTableClient returns a new TableClient.
func NewTableClient(ctx context.Context, cfg Config) (chunk.TableClient, error) {
	opts := toOptions(cfg.GRPCClientConfig.DialOption(bigtableInstrumentation()))
	client, err := bigtable.NewAdminClient(ctx, cfg.Project, cfg.Instance, opts...)
	if err != nil {
		return nil, err
	}
	return &tableClient{
		cfg:    cfg,
		client: client,

		tableInfo: map[string]*bigtable.TableInfo{},
	}, nil
}

func (c *tableClient) ListTables(ctx context.Context) ([]string, error) {
	tables, err := c.client.Tables(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "client.Tables")
	}

	if c.tableExpiration.Before(time.Now()) {
		c.tableInfo = map[string]*bigtable.TableInfo{}
		c.tableExpiration = time.Now().Add(c.cfg.TableCacheExpiration)
	}

	// Check each table has the right column family.  If not, omit it.
	output := make([]string, 0, len(tables))
	for _, table := range tables {
		info, exists := c.tableInfo[table]
		if !exists || !c.cfg.TableCacheEnabled {
			info, err = c.client.TableInfo(ctx, table)
			if err != nil {
				return nil, errors.Wrap(err, "client.TableInfo")
			}
		}

		if hasColumnFamily(info.FamilyInfos) {
			output = append(output, table)
			continue
		}
		c.tableInfo[table] = info
	}

	return output, nil
}

func hasColumnFamily(infos []bigtable.FamilyInfo) bool {
	for _, family := range infos {
		if family.Name == columnFamily {
			return true
		}
	}
	return false
}

func (c *tableClient) CreateTable(ctx context.Context, desc chunk.TableDesc) error {
	if err := c.client.CreateTable(ctx, desc.Name); err != nil {
		if !alreadyExistsError(err) {
			return errors.Wrap(err, "client.CreateTable")
		}
	}

	if err := c.client.CreateColumnFamily(ctx, desc.Name, columnFamily); err != nil {
		if !alreadyExistsError(err) {
			return errors.Wrap(err, "client.CreateColumnFamily")
		}
	}

	return nil
}

func alreadyExistsError(err error) bool {
	serr, ok := status.FromError(err)
	return ok && serr.Code() == codes.AlreadyExists
}

func (c *tableClient) DeleteTable(ctx context.Context, name string) error {
	if err := c.client.DeleteTable(ctx, name); err != nil {
		return errors.Wrap(err, "client.DeleteTable")
	}
	delete(c.tableInfo, name)

	return nil
}

func (c *tableClient) DescribeTable(ctx context.Context, name string) (desc chunk.TableDesc, isActive bool, err error) {
	return chunk.TableDesc{
		Name: name,
	}, true, nil
}

func (c *tableClient) UpdateTable(ctx context.Context, current, expected chunk.TableDesc) error {
	return nil
}
