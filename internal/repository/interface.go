package repository

import (
	"context"
	"snmpflapd/internal/repository/flapdb"
)

var _ Connector = &flapdb.Connector{}

// Connector is an object to connect the database
type Connector interface {
	// CleanUp deletes old cached values from DB
	CleanUp(ctx context.Context) error

	// Close connection to db
	Close()

	SaveLinkEvent(*flapdb.Model) error

	UpdateLinkEvent(*flapdb.Model) error

	GetCachedIfName(*flapdb.Model) (*string, error)

	PutCachedIfName(context.Context, *flapdb.Model) error

	GetCachedIfAlias(*flapdb.Model) (*string, error)

	PutCachedIfAlias(context.Context, *flapdb.Model) error

	GetCachedHostname(*flapdb.Model) (*string, error)

	PutCachedHostname(context.Context, *flapdb.Model) error
}
