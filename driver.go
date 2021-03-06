package main

import (
	"fmt"

	"github.com/ninjasphere/go-ninja/api"
	"github.com/ninjasphere/go-ninja/logger"
	"github.com/ninjasphere/go-ninja/support"

	"github.com/ninjasphere/go-openzwave"
	"github.com/ninjasphere/go-openzwave/NT"
)

const (
	driverName = "com.ninjablocks.zwave"
)

var (
	info = ninja.LoadModuleInfo("./package.json")
)

type ZDriver struct {
	support.DriverSupport
	config   *Zconfig
	debug    bool
	zwaveAPI openzwave.API
	exit     chan int
}

type Zconfig struct {
}

func defaultConfig() *Zconfig {
	return &Zconfig{}
}

func (driver *ZDriver) ZWave() openzwave.API {
	return driver.zwaveAPI
}

func (driver *ZDriver) Ninja() ninja.Driver {
	return driver
}

func (driver *ZDriver) Connection() *ninja.Connection {
	return driver.Conn
}

func newZWaveDriver(debug bool) (*ZDriver, error) {

	driver := &ZDriver{
		config:   defaultConfig(),
		debug:    debug,
		zwaveAPI: nil,
		exit:     make(chan int, 0),
	}

	err := driver.Init(info)
	if err != nil {
		return nil, err
	}

	err = driver.Export(driver)
	if err != nil {
		return nil, err
	}

	return driver, nil
}

func (d *ZDriver) Start(config *Zconfig) error {
	d.Log.Infof("Driver %s starting with config %v", driverName, config)

	d.config = config

	zwaveDeviceFactory := func(api openzwave.API, node openzwave.Node) openzwave.Device {
		d.zwaveAPI = api
		return GetLibrary().GetDeviceFactory(*node.GetProductId())(d, node)
	}

	shuttingDown := false

	notificationCallback := func(api openzwave.API, nt openzwave.Notification) {
		switch nt.GetNotificationType().Code {
		case NT.NODE_REMOVED:
			//
			// Currently the RPC layer prevents us releasing the resources associated
			// with removed nodes. If the nodes come back (when, say, the zwave controller
			// is re-inserted), we can't build new device  wrappers for them because the
			// devices are already registered with the RPC layer.
			//
			// We could fix the RPC layer or we could attempt to work around the
			// problems with the RPC layer by using "patch" proxies for each ninja device
			// that allows us to change the actual zwave device.
			//
			// For now, it is simpler if we simply restart the driver process in the event of node
			// removal. This also avoids potential race conditions between
			// event dispatch and freeing of the resources associated with the
			// removed node.
			//
			if !shuttingDown {
				shuttingDown = true
				api.Logger().Infof("ZWave driver shutdown in response to node removed event.")
				api.Shutdown(openzwave.EXIT_NODE_REMOVED)
			}
		default:

		}
	}

	configurator := openzwave.
		BuildAPI("/usr/local/etc/openzwave", ".", "").
		SetLogger(logger.GetLogger(fmt.Sprintf("%s.backend", d.Info.ID))).
		SetNotificationCallback(notificationCallback).
		SetDeviceFactory(zwaveDeviceFactory)

	if d.debug {
		callback := func(api openzwave.API, notification openzwave.Notification) {
			api.Logger().Infof("%v\n", notification)
			notificationCallback(api, notification)
		}

		configurator.SetNotificationCallback(callback)
	}

	go func() {
		// slightly racy - we would like a guarantee we have replied to Start
		// before we start generating advice about new nodes.
		d.exit <- configurator.Run()
	}()

	d.SendEvent("config", config)

	return nil
}

func (d *ZDriver) Stop() error {
	d.Log.Infof("Stop received - shutting down")
	d.zwaveAPI.Shutdown(0)
	return nil
}

// wait until the drivers are ready for us to shutdown.
func (d *ZDriver) wait() int {
	return <-d.exit
}
