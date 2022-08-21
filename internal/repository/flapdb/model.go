package flapdb

import (
	"fmt"
	"net"
	"time"
)

const (
	ifAdminStatusUP   = 1
	ifAdminStatusDOWN = 2
	ifOperStatusUP    = 1
	// ifOperStatusDOWN  = 2
)

type Model struct {
	Sid           string
	IfIndex       int
	IfAdminStatus int
	IfOperStatus  int
	IfName        *string
	IfAlias       *string
	HostName      *string
	IpAddress     net.IP
	Time          time.Time
	TimeTicks     uint
}

func (le *Model) String() string {
	eventTime := le.Time.Format("2006-01-02 15:04:05")

	hostName := le.IpAddress.String()
	if le.HostName != nil {
		hostName = *le.HostName
	}

	ifName := "NULL"
	if le.IfName != nil {
		ifName = *le.IfName
	}
	ifAlias := "NULL"
	if le.IfAlias != nil {
		ifAlias = *le.IfAlias
	}

	return fmt.Sprintf("eventTime=%s host=%s ifName=%s ifIndex=%d ifAlias=%s status=%s",
		eventTime, hostName, ifName, le.IfIndex, ifAlias, le.ifStateText())
}

func (le *Model) ifStateText() string {
	var ifState string

	switch le.IfAdminStatus {
	case ifAdminStatusDOWN:
		ifState = "admin down"

	case ifAdminStatusUP:
		switch le.IfOperStatus {
		case ifOperStatusUP:
			ifState = "up"
		default:
			ifState = "down"
		}
	}
	return ifState
}
