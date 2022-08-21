package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"snmpflapd/internal/repository/flapdb"
	"snmpflapd/internal/services/dbcleanup"
	"snmpflapd/internal/services/linkevent"
	"strconv"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	g "github.com/gosnmp/gosnmp"
)

const (
	defaultConfigFilename = "settings.conf"
	defaultLogFilename    = "snmpflapd.log"
	defaultListenAddress  = "0.0.0.0"
	defaultListenPort     = 162
	defaultDBHost         = "127.0.0.1"
	defaultDBUser         = "root"
	defaultDBName         = "snmpflapd"
	defaultDBPassword     = ""
	defaultCommunity      = ""
	// queueInterval          = 30
	defaultCleanUpInterval = 60
)

type Config struct {
	LogFilename     string
	ListenAddress   string
	ListenPort      int
	DBHost          string
	DBName          string
	DBUser          string
	DBPassword      string
	Community       string
	CleanUpInterval int
}

// flags
var (
	version            string
	build              string
	flagVerbose        bool
	flagConfigFilename string
	flagVersion        bool
	period             time.Duration = time.Hour * 6
)

var config = Config{
	LogFilename:     defaultLogFilename,
	ListenAddress:   defaultListenAddress,
	ListenPort:      defaultListenPort,
	DBHost:          defaultDBHost,
	DBName:          defaultDBName,
	DBUser:          defaultDBUser,
	DBPassword:      defaultDBPassword,
	Community:       defaultCommunity,
	CleanUpInterval: defaultCleanUpInterval,
}

func init() {

	// Reading flags
	flag.StringVar(&flagConfigFilename, "f", defaultConfigFilename, "Location of config file")
	flag.BoolVar(&flagVerbose, "v", false, "Enable verbose logging")
	flag.BoolVar(&flagVersion, "V", false, "Print version information and quit")
	flag.Parse()

	// Reading config
	readConfigFile(&flagConfigFilename)
	readConfigEnv()

}

func main() {

	ctx, cancel := context.WithCancel(context.TODO())

	if flagVersion {
		build := fmt.Sprintf("FlapMyPort snmpflapd version %s, build %s", version, build)
		fmt.Println(build)
		os.Exit(0)
	}

	var err error

	// Logging setup
	f, err := os.OpenFile(config.LogFilename, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Println(err)
		log.Fatalln(err)
	}
	defer f.Close()
	log.SetOutput(f)
	log.Println("snmpflapd started")

	connector, err := flapdb.MakeDB(&flapdb.Config{
		Host:     config.DBHost,
		DBName:   config.DBName,
		User:     config.DBUser,
		Password: config.DBPassword,
	})
	if err != nil {
		fmt.Println(err)
		log.Fatalln(err)
	}
	defer connector.Close()

	// Periodic DB clean up
	go dbcleanup.RunDBCleanUp(ctx, connector, period)

	tl := g.NewTrapListener()
	tl.OnNewTrap = func(packet *g.SnmpPacket, addr *net.UDPAddr) {
		if linkevent.IsLinkEvent(packet) {
			go linkevent.LinkEventHandler(ctx, connector, packet, addr, config.Community)
		}
	}
	tl.Params = g.Default

	listenSocket := fmt.Sprintf("%v:%v", config.ListenAddress, config.ListenPort)
	tlErr := tl.Listen(listenSocket)
	if tlErr != nil {
		fmt.Println(tlErr)
		log.Fatalln(tlErr)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	<-c

	defer func() {
		cancel()
	}()
}

func readConfigFile(file *string) {
	if _, err := toml.DecodeFile(*file, &config); err != nil {
		msg := fmt.Sprintf("%s not found. Suppose we're using environment variables", *file)
		fmt.Println(msg)
		log.Println(msg)
	}
}

func readConfigEnv() {

	if logFilename, exists := os.LookupEnv("LOGFILE"); exists {
		config.LogFilename = logFilename
	}

	if listenAddress, exists := os.LookupEnv("LISTEN_ADDRESS"); exists {
		config.ListenAddress = listenAddress
	}

	if listenPort, exists := os.LookupEnv("LISTEN_PORT"); exists {
		if intPort, error := strconv.Atoi(listenPort); error != nil {
			msg := "Wrong environment variable LISTEN_PORT"
			fmt.Println(msg)
			log.Fatalln(msg)

		} else {
			config.ListenPort = intPort
		}

	}

	if dbHost, exists := os.LookupEnv("DBHOST"); exists {
		config.DBHost = dbHost
	}

	if dbName, exists := os.LookupEnv("DBNAME"); exists {
		config.DBName = dbName
	}

	if dbUser, exists := os.LookupEnv("DBUSER"); exists {
		config.DBUser = dbUser
	}

	if dbPassword, exists := os.LookupEnv("DBPASSWORD"); exists {
		config.DBPassword = dbPassword
	}

	if community, exists := os.LookupEnv("COMMUNITY"); exists {
		config.Community = community
	}

}

// func logVerbose(s string) {
// 	if flagVerbose {
// 		log.Print(s)
// 	}
// }
