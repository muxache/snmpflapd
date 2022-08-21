package flapdb

import (
	"context"
	"fmt"
	"log"
	"sync"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

// Connector is an object to connect the database
type Connector struct {
	db                   *sqlx.DB
	mx                   sync.Mutex
	cacheIfNameMinutes   int
	cacheIfAliasMinutes  int
	cacheHostnameMinutes int
}

type Config struct {
	CacheIfNameMinutes           int
	CacheIfAliasMinutes          int
	CacheHostnameMinutes         int
	Host, DBName, User, Password string
}

// MakeDB returns an SQL Connector object to make queries
func MakeDB(cfg *Config) (*Connector, error) {

	dataSourceName := fmt.Sprintf("%s:%s@tcp(%s)/%s", cfg.User, cfg.Password, cfg.Host, cfg.DBName)
	db, err := sqlx.Open("mysql", dataSourceName)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return &Connector{
		db:                   db,
		cacheIfNameMinutes:   cfg.CacheIfNameMinutes,
		cacheIfAliasMinutes:  cfg.CacheIfAliasMinutes,
		cacheHostnameMinutes: cfg.CacheHostnameMinutes,
	}, nil
}

// CleanUp deletes old cached values from DB
func (c *Connector) CleanUp(ctx context.Context) error {

	c.mx.Lock()
	defer c.mx.Unlock()

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Println(err)
		}
	}()

	log.Printf("Cleanup DB started")

	if _, err := tx.ExecContext(ctx, cleanUpHostnameSQL, c.cacheHostnameMinutes); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, cleanUpIfNameSQL, c.cacheIfNameMinutes); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, cleanUpIfAliasSQL, c.cacheIfAliasMinutes); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (c *Connector) SaveLinkEvent(le *Model) error {

	if le.TimeTicks == 0 {
		log.Println("SNMP Trap has no timeTicks", le)
	}

	ifAdminStatus, ifOperStatus := "down", "down"
	if le.IfAdminStatus == ifAdminStatusUP {
		ifAdminStatus = "up"
	}

	if le.IfOperStatus == ifOperStatusUP {
		ifOperStatus = "up"
	}

	sql := `INSERT INTO ports 
			(ipaddress, hostname, ifIndex, ifName, ifAlias, ifAdminStatus, ifOperStatus, time, sid, timeTicks)
			VALUES 
			(:ipaddress, :hostname, :ifIndex, :ifName, :ifAlias, :ifAdminStatus, :ifOperStatus, :time, :sid, :timeTicks)`

	args := map[string]interface{}{
		"ipaddress":     le.IpAddress.String(),
		"hostname":      le.HostName,
		"ifIndex":       le.IfIndex,
		"ifName":        le.IfName,
		"ifAlias":       le.IfAlias,
		"ifAdminStatus": ifAdminStatus,
		"ifOperStatus":  ifOperStatus,
		"time":          le.Time.Format("2006-01-02 15:04:05"),
		"sid":           le.Sid,
		"timeTicks":     le.TimeTicks}

	c.mx.Lock()
	defer c.mx.Unlock()

	if _, err := c.db.NamedExec(sql, args); err != nil {
		log.Println(le.Sid, "unable to exec SQL query", err)
		return err
	}

	// logVerbose(fmt.Sprintln(le.sid, "link event saved", le.String()))
	return nil
}

func (c *Connector) UpdateLinkEvent(le *Model) error {

	sql := `UPDATE ports SET  hostname = :hostname, ifName = :ifName, ifAlias = :ifAlias WHERE sid = :sid;`

	args := map[string]interface{}{
		"hostname": le.HostName,
		"ifAlias":  le.IfAlias,
		"ifName":   le.IfName,
		"sid":      le.Sid}

	c.mx.Lock()
	defer c.mx.Unlock()

	if _, err := c.db.NamedExec(sql, args); err != nil {
		log.Println(le.Sid, "unable to exec SQL query", err)
		return err
	}

	// logVerbose(fmt.Sprintln(le.sid, "link event updated", le.String()))
	return nil
}

func (c *Connector) GetCachedIfName(le *Model) (*string, error) {

	c.mx.Lock()
	defer c.mx.Unlock()

	sql := "SELECT ifName FROM cache_ifname	WHERE time > now() - INTERVAL ? MINUTE AND ipaddress = ? AND ifIndex = ?;"

	cachedIfName := ""
	if err := c.db.Get(&cachedIfName, sql, c.cacheIfNameMinutes, le.IpAddress.String(), le.IfIndex); err != nil {
		// logVerbose(fmt.Sprintln(le.sid, "no cached ifName"))
		return nil, err
	}

	return &cachedIfName, nil
}

func (c *Connector) PutCachedIfName(ctx context.Context, m *Model) error {

	c.mx.Lock()
	defer c.mx.Unlock()

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Println(err)
		}
	}()

	if _, err := tx.ExecContext(ctx, deleteIfnameIfindex, m.IpAddress.String(), m.IfIndex); err != nil {
		log.Println(m.Sid, err)
		return err
	}

	if _, err := c.db.ExecContext(ctx, setCacheIfName, m.IpAddress.String(), m.IfIndex, m.IfName); err != nil {
		log.Println(m.Sid, err, m.String())
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// logVerbose(fmt.Sprintf("%s put values ('%s', '%d', '%d') to cache_ifname", le.sid, *le.ifName, le.ifIndex, le.hostName))

	return nil
}

func (c *Connector) GetCachedIfAlias(le *Model) (*string, error) {

	c.mx.Lock()
	defer c.mx.Unlock()

	sql := "SELECT ifAlias FROM cache_ifalias WHERE time > now() - INTERVAL ? MINUTE AND ipaddress = ? AND ifIndex = ?;"

	cachedIfAlias := ""
	if err := c.db.Get(&cachedIfAlias, sql, c.cacheIfAliasMinutes, le.IpAddress.String(), le.IfIndex); err != nil {
		// logVerbose(fmt.Sprintln(le.sid, "no cached ifAlias"))
		return nil, err
	}

	return &cachedIfAlias, nil
}

func (c *Connector) PutCachedIfAlias(ctx context.Context, m *Model) error {

	c.mx.Lock()
	defer c.mx.Unlock()

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Println(err)
		}
	}()

	if _, err := tx.ExecContext(ctx, deleteIfAliasIfindex, m.IpAddress.String(), m.IfIndex); err != nil {
		log.Println(m.Sid, err)
		return err
	}

	if _, err := tx.ExecContext(ctx, setCacheIfAlias, m.IpAddress.String(), m.IfIndex, m.IfAlias); err != nil {
		log.Println(m.Sid, err)
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// logVerbose(fmt.Sprintf("%s put values ('%s', '%d', '%s') to cache_ifalias", le.sid, *le.ifAlias, le.ifIndex, le.ipAddress))

	return nil
}

func (c *Connector) GetCachedHostname(le *Model) (*string, error) {

	c.mx.Lock()
	defer c.mx.Unlock()

	var cachedHostname string
	if err := c.db.Get(&cachedHostname, selecthostnameWhereTime, c.cacheHostnameMinutes, le.IpAddress.String()); err != nil {
		// logVerbose(fmt.Sprintln(le.sid, "no cached hostname"))
		return nil, err
	}

	return &cachedHostname, nil
}

func (c *Connector) PutCachedHostname(ctx context.Context, m *Model) error {

	c.mx.Lock()
	defer c.mx.Unlock()

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Println(err)
		}
	}()

	if _, err := tx.ExecContext(ctx, deleteHostNameWhereIPaddr, m.IpAddress.String()); err != nil {
		log.Println(m.Sid, err)
		return err
	}

	if _, err := tx.ExecContext(ctx, setCacheHostName, m.IpAddress.String(), m.HostName); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// logVerbose(fmt.Sprintf("%s put values ('%s', '%s') to cache_hostname", le.sid, *le.hostName, le.ipAddress))

	return nil
}

func (c *Connector) Close() {
	c.db.Close()
}

const (
	cleanUpHostnameSQL        = `DELETE FROM cache_hostname WHERE time < now() - INTERVAL ? MINUTE;`
	cleanUpIfNameSQL          = `DELETE FROM cache_ifname WHERE time < now() - INTERVAL ? MINUTE;`
	cleanUpIfAliasSQL         = `DELETE FROM cache_ifalias WHERE time < now() - INTERVAL ? MINUTE;`
	deleteIfnameIfindex       = `DELETE FROM cache_ifname WHERE ipaddress = ? and ifIndex = ?;`
	setCacheIfName            = `INSERT INTO cache_ifname (ipaddress, ifIndex, ifName) VALUES (?, ?, ?);`
	deleteIfAliasIfindex      = `DELETE FROM cache_ifalias WHERE ipaddress = ? and ifindex = ?;`
	setCacheIfAlias           = `INSERT INTO cache_ifalias (ipaddress, ifIndex, ifAlias) VALUES (?, ?, ?);`
	selecthostnameWhereTime   = "SELECT hostname FROM cache_hostname WHERE time > now() - INTERVAL ? MINUTE AND ipaddress = ?;"
	deleteHostNameWhereIPaddr = `DELETE FROM cache_hostname WHERE ipaddress = ?;`
	setCacheHostName          = `INSERT INTO cache_hostname (ipaddress, hostname) VALUES (?, ?);`
)
