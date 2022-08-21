// This file is responsible for handling linkUP/linkDown snmp traps.
// It performed the following actions:
// - creates a linkEvent from an snmpPacket
// - fills it's fields with additional info, missing in the snmpPacket
// - reads/writes to cache DB tables using the *Connector
// - finally, stores linkEvents to a database using the *Connector

package linkevent

import (
	"context"
	"log"
	"net"
	"snmpflapd/internal/repository"
	"snmpflapd/internal/repository/flapdb"
	"strconv"
	"strings"
	"time"

	"github.com/chilts/sid"
	g "github.com/gosnmp/gosnmp"
)

const (
	oidReference             = ".1.3.6.1.6.3.1.1.4.1.0"
	timeTicksReference       = ".1.3.6.1.2.1.1.3.0"
	linkUP                   = ".1.3.6.1.6.3.1.1.5.4"
	linkDOWN                 = ".1.3.6.1.6.3.1.1.5.3"
	ifIndexOIDPrefix         = ".1.3.6.1.2.1.2.2.1.1"
	ifNameOIDPrefix          = ".1.3.6.1.2.1.31.1.1.1.1."
	ifAliasOIDPrefix         = ".1.3.6.1.2.1.31.1.1.1.18."
	ifAdminStatusOIDPrefix   = ".1.3.6.1.2.1.2.2.1.7"
	ifOperStatusOIDPrefix    = ".1.3.6.1.2.1.2.2.1.8"
	ifNameVarBindPrefixJunOS = ".1.3.6.1.2.1.31.1.1.1.1"
	sysNameOID               = ".1.3.6.1.2.1.1.5.0"
)

var (
	snmpSema RequestSemaphore
)

type LinkEvent struct {
	sid           string
	ifIndex       int
	ifAdminStatus int
	ifOperStatus  int
	ifName        *string
	ifAlias       *string
	hostName      *string
	ipAddress     net.IP
	time          time.Time
	timeTicks     uint

	repo      repository.Connector
	community string
}

// FromSnmpPacket returns linkEvent from SnmpPacket and net.UDPAddr
func (le *LinkEvent) FromSnmpPacket(p *g.SnmpPacket, addr net.IP) {
	if !IsLinkEvent(p) {
		// Don't waste my CPU time!
		return
	}

	le.ipAddress = addr

	// Fill the linkEvent with variables from a packet
	for _, variable := range p.Variables {

		if strings.Contains(variable.Name, ifIndexOIDPrefix) {
			le.ifIndex = variable.Value.(int)
			continue
		}

		if strings.Contains(variable.Name, ifAdminStatusOIDPrefix) {
			le.ifAdminStatus = variable.Value.(int)
			continue
		}

		if strings.Contains(variable.Name, ifOperStatusOIDPrefix) {
			le.ifOperStatus = variable.Value.(int)
			continue
		}

		if strings.Contains(variable.Name, ifNameVarBindPrefixJunOS) {
			ifNameBytes, ok := variable.Value.([]uint8)
			ifName := string(ifNameBytes)
			if ok {
				le.ifName = &ifName
			} else {
				log.Println(le, "empty ifNameVarBindPrefixJunOS")
			}
			continue
		}

		if strings.Contains(variable.Name, timeTicksReference) {

			timeTicks, ok := variable.Value.(uint)

			if ok {
				le.timeTicks = timeTicks
			} else {
				log.Println(le, "missing timeTicks in the SNMP trap")
			}
			continue
		}

	}
}

// LinkEventHandler handles linkUP/linkDOWN snmp traps
func LinkEventHandler(ctx context.Context, repo repository.Connector, p *g.SnmpPacket, addr *net.UDPAddr, community string) {
	event := LinkEvent{time: time.Now().Local(), repo: repo, community: community}
	event.sid = sid.Id() // This is for unique trap identification
	event.FromSnmpPacket(p, addr.IP)

	// logVerbose(fmt.Sprintln(event.sid, "trap received:", event.String()))

	if err := event.saveLinkEvent(); err != nil {
		log.Println(event.sid, "unable to save link event", err)
		return
	}

	// Fetch missing data and update the linkEvent
	event.FetchMissingData(ctx)
	if err := event.updateLinkEvent(); err != nil {
		log.Println(event.sid, "unable to update link event:", err)
	}

}

// getEventOID returns oid from OID Reference that is in an SnmpPacket
func getEventOID(p *g.SnmpPacket) string {
	for _, variable := range p.Variables {
		if variable.Name == oidReference {
			return variable.Value.(string)
		}
	}
	return ""
}

// isLinkEvent returns true if an SNMP trap is about Link UP/DOWN event
func IsLinkEvent(p *g.SnmpPacket) bool {
	eventOid := getEventOID(p)
	if eventOid == linkUP || eventOid == linkDOWN {
		return true
	}
	return false
}

func (le *LinkEvent) FetchMissingData(ctx context.Context) {

	// logVerbose(fmt.Sprintln(le.sid, "fetching missing data"))

	if le.hostName == nil {
		le.FillHostName(ctx)
	}

	if le.ifName == nil {
		le.FillIfName(ctx)
	}

	if le.ifAlias == nil {
		le.FillIfAlias(ctx)
	}
}

// FillHostName tries to get a hostname from cache, then from the device via SNMP request
func (le *LinkEvent) FillHostName(ctx context.Context) {

	// logVerbose(fmt.Sprintln(le.sid, "filling hostname"))

	// 1. Try to get the value from cache
	if le.getCachedHostname() {
		// logVerbose(fmt.Sprintln(le.sid, "used cached hostName", *le.hostName))
		return
	}

	// 2. Get value from SNMP and put it to the cache
	if hostName, err := getSNMPString(sysNameOID, le.ipAddress, le.community); err != nil {
		log.Println(le.sid, "unable to get hostname via SNMP:", err)
		return

	} else {
		le.hostName = hostName
		// logVerbose(fmt.Sprintf("%s received hostname '%s' from %s via SNMP", le.sid, *le.hostName, le.ipAddress))
	}

	if err := le.putCachedHostname(ctx); err != nil {
		return
	}

}

// FillHostName tries to get a ifName from cache, then from the device via SNMP request
func (le *LinkEvent) FillIfName(ctx context.Context) {

	// logVerbose(fmt.Sprintln(le.sid, "filling ifName"))

	// 1. Try to get the value from cache
	if le.getCachedIfName() {
		// logVerbose(fmt.Sprintf("%s used cached ifName %s", le.sid, *le.ifName))
		return
	}

	// 2. Get value from SNMP and put it to the cache
	if ifName, err := getSNMPString(ifNameOIDPrefix+strconv.Itoa(le.ifIndex), le.ipAddress, le.community); err != nil {
		log.Println(le.sid, "unable to get ifName vie SNMP:", err)
		return

	} else {
		le.ifName = ifName
		// logVerbose(fmt.Sprintf("%s received ifName '%s' from %s via SNMP", le.sid, *le.ifName, le.ipAddress))
	}

	if err := le.putCachedIfName(ctx); err != nil {
		return
	}

}

// FillIfAlias tries to get an ifAlias from cache, then from the device via SNMP request
func (le *LinkEvent) FillIfAlias(ctx context.Context) {

	// logVerbose(fmt.Sprintln(le.sid, "filling ifAlias"))

	// 1. Try to get the value from cache
	if le.getCachedIfAlias() {
		// logVerbose(fmt.Sprintf("%s used cached ifAlias '%s'", le.sid, *le.ifAlias))
		return
	}

	// 2. Get value from SNMP and put it to the cache
	ifAlias, err := getSNMPString(ifAliasOIDPrefix+strconv.Itoa(le.ifIndex), le.ipAddress, le.community)
	if err != nil {
		log.Println(le.sid, "unable to get ifAlias via SNMP:", err)
		return

	} else {
		le.ifAlias = ifAlias
		// logVerbose(fmt.Sprintf("%s received ifAlias '%s' from %s via SNMP", le.sid, *ifAlias, &le.ipAddress))
	}

	if err := le.putCachedIfAlias(ctx); err != nil {
		return
	}
}

func (le *LinkEvent) saveLinkEvent() error {

	if le.timeTicks == 0 {
		log.Println("SNMP Trap has no timeTicks", le)
	}

	model := &flapdb.Model{
		IpAddress:     le.ipAddress,
		HostName:      le.hostName,
		IfIndex:       le.ifIndex,
		IfName:        le.ifName,
		IfAlias:       le.ifAlias,
		IfAdminStatus: le.ifAdminStatus,
		IfOperStatus:  le.ifOperStatus,
		Time:          le.time,
		Sid:           le.sid,
		TimeTicks:     le.timeTicks,
	}
	if err := le.repo.SaveLinkEvent(model); err != nil {
		return err
	}

	return nil
}

func (le *LinkEvent) updateLinkEvent() error {

	model := &flapdb.Model{
		HostName: le.hostName,
		IfName:   le.ifName,
		IfAlias:  le.ifAlias,
		Sid:      le.sid,
	}
	if err := le.repo.UpdateLinkEvent(model); err != nil {
		log.Println(le.sid, "unable to exec SQL query", err)
		return err
	}

	// logVerbose(fmt.Sprintln(le.sid, "link event updated", le.String()))
	return nil
}

func (le *LinkEvent) getCachedIfName() bool {

	model := &flapdb.Model{
		IpAddress: le.ipAddress,
		IfIndex:   le.ifIndex,
	}
	cachedIfName, err := le.repo.GetCachedIfName(model)
	if err != nil {
		// logVerbose(fmt.Sprintln(le.sid, "no cached ifName"))
		return false
	}

	le.ifName = cachedIfName

	return true
}

func (le *LinkEvent) putCachedIfName(ctx context.Context) error {

	model := &flapdb.Model{
		IpAddress: le.ipAddress,
		IfIndex:   le.ifIndex,
	}
	if err := le.repo.PutCachedIfName(ctx, model); err != nil {
		log.Println(le.sid, err)
		return err
	}

	// logVerbose(fmt.Sprintf("%s put values ('%s', '%d', '%d') to cache_ifname", le.sid, *le.ifName, le.ifIndex, le.hostName))
	return nil
}

func (le *LinkEvent) getCachedIfAlias() bool {

	model := &flapdb.Model{
		IpAddress: le.ipAddress,
		IfIndex:   le.ifIndex,
	}

	cachedIfAlias, err := le.repo.GetCachedIfAlias(model)
	if err != nil {
		// logVerbose(fmt.Sprintln(le.sid, "no cached ifAlias"))
		return false
	}
	le.ifAlias = cachedIfAlias
	return true
}

func (le *LinkEvent) putCachedIfAlias(ctx context.Context) error {

	model := &flapdb.Model{
		IpAddress: le.ipAddress,
		IfIndex:   le.ifIndex,
		IfAlias:   le.ifAlias,
	}
	if err := le.repo.PutCachedIfAlias(ctx, model); err != nil {
		log.Println(le.sid, err)
		return err
	}

	// logVerbose(fmt.Sprintf("%s put values ('%s', '%d', '%s') to cache_ifalias", le.sid, *le.ifAlias, le.ifIndex, le.ipAddress))

	return nil

}

func (le *LinkEvent) getCachedHostname() bool {
	model := &flapdb.Model{
		IpAddress: le.ipAddress,
	}

	cachedHostname, err := le.repo.GetCachedHostname(model)
	if err != nil {
		// logVerbose(fmt.Sprintln(le.sid, "no cached hostname"))
		return false
	}

	le.hostName = cachedHostname

	return true
}

func (le *LinkEvent) putCachedHostname(ctx context.Context) error {

	model := &flapdb.Model{
		IpAddress: le.ipAddress,
		HostName:  le.hostName,
	}

	if err := le.repo.PutCachedHostname(ctx, model); err != nil {
		log.Println(le.sid, err)
		return err
	}

	// logVerbose(fmt.Sprintf("%s put values ('%s', '%s') to cache_hostname", le.sid, *le.hostName, le.ipAddress))

	return nil
}
