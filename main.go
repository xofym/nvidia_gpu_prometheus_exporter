package main

import (
	"flag"
	"net/http"
	"strconv"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/xofym/gonvml"
)

const (
	namespace = "nvidia_gpu"
)

var (
	addr  = flag.String("web.listen-address", ":9445", "Address to listen on for web interface and telemetry.")
	debug = flag.Bool("log.debug", false, "sets log level to debug")

	labels = []string{"minor_number", "uuid", "name"}
)

type Collector struct {
	sync.Mutex
	numDevices  prometheus.Gauge
	usedMemory  *prometheus.GaugeVec
	totalMemory *prometheus.GaugeVec
	dutyCycle   *prometheus.GaugeVec
	powerUsage  *prometheus.GaugeVec
	temperature *prometheus.GaugeVec
	fanSpeed    *prometheus.GaugeVec
}

func NewCollector() *Collector {
	return &Collector{
		numDevices: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "num_devices",
				Help:      "Number of GPU devices",
			},
		),
		usedMemory: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "memory_used_bytes",
				Help:      "Memory used by the GPU device in bytes",
			},
			labels,
		),
		totalMemory: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "memory_total_bytes",
				Help:      "Total memory of the GPU device in bytes",
			},
			labels,
		),
		dutyCycle: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "duty_cycle",
				Help:      "Percent of time over the past sample period during which one or more kernels were executing on the GPU device",
			},
			labels,
		),
		powerUsage: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "power_usage_milliwatts",
				Help:      "Power usage of the GPU device in milliwatts",
			},
			labels,
		),
		temperature: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "temperature_celsius",
				Help:      "Temperature of the GPU device in celsius",
			},
			labels,
		),
		fanSpeed: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "fanspeed_percent",
				Help:      "Fanspeed of the GPU device as a percent of its maximum",
			},
			labels,
		),
	}
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.numDevices.Desc()
	c.usedMemory.Describe(ch)
	c.totalMemory.Describe(ch)
	c.dutyCycle.Describe(ch)
	c.powerUsage.Describe(ch)
	c.temperature.Describe(ch)
	c.fanSpeed.Describe(ch)
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	// Only one Collect call in progress at a time.
	c.Lock()
	defer c.Unlock()

	c.usedMemory.Reset()
	c.totalMemory.Reset()
	c.dutyCycle.Reset()
	c.powerUsage.Reset()
	c.temperature.Reset()
	c.fanSpeed.Reset()

	numDevices, err := gonvml.DeviceCount()
	if err != nil {
		log.Error().Err(err).Msg("Cannot get DeviceCount")
		return
	} else {
		c.numDevices.Set(float64(numDevices))
		ch <- c.numDevices
	}

	for i := 0; i < int(numDevices); i++ {
		// Device information
		dev, err := gonvml.DeviceHandleByIndex(uint(i))
		if err != nil {
			log.Warn().
				Err(err).
				Int("device_index", i).
				Msg("Cannot get DeviceHandleByIndex")
			continue
		}

		minorNumber, err := dev.MinorNumber()
		if err != nil {
			log.Warn().
				Err(err).
				Int("device_index", i).
				Msg("Cannot get device MinorNumber")
			continue
		}
		minor := strconv.Itoa(int(minorNumber))

		uuid, err := dev.UUID()
		if err != nil {
			log.Warn().
				Err(err).
				Int("device_index", i).
				Msg("Cannot get device UUID")
			continue
		}

		name, err := dev.Name()
		if err != nil {
			log.Warn().
				Err(err).
				Int("device_index", i).
				Msg("Cannot get device Name")
			continue
		}

		// Metrics
		totalMemory, usedMemory, err := dev.MemoryInfo()
		if err != nil {
			log.Debug().
				Err(err).
				Int("device_index", i).
				Msg("Cannot get MemoryInfo")
		} else {
			c.usedMemory.WithLabelValues(minor, uuid, name).Set(float64(usedMemory))
			c.totalMemory.WithLabelValues(minor, uuid, name).Set(float64(totalMemory))
		}

		dutyCycle, _, err := dev.UtilizationRates()
		if err != nil {
			log.Debug().
				Err(err).
				Int("device_index", i).
				Msg("Cannot get UtilizationRates")
		} else {
			c.dutyCycle.WithLabelValues(minor, uuid, name).Set(float64(dutyCycle))
		}

		powerUsage, err := dev.PowerUsage()
		if err != nil {
			log.Debug().
				Err(err).
				Int("device_index", i).
				Msg("Cannot get PowerUsage")
		} else {
			c.powerUsage.WithLabelValues(minor, uuid, name).Set(float64(powerUsage))
		}

		temperature, err := dev.Temperature()
		if err != nil {
			log.Debug().
				Err(err).
				Int("device_index", i).
				Msg("Cannot get Temperature")
		} else {
			c.temperature.WithLabelValues(minor, uuid, name).Set(float64(temperature))
		}

		fanSpeed, err := dev.FanSpeed()
		if err != nil {
			log.Debug().
				Err(err).
				Int("device_index", i).
				Msg("Cannot get FanSpeed")
		} else {
			c.fanSpeed.WithLabelValues(minor, uuid, name).Set(float64(fanSpeed))
		}
	}
	c.usedMemory.Collect(ch)
	c.totalMemory.Collect(ch)
	c.dutyCycle.Collect(ch)
	c.powerUsage.Collect(ch)
	c.temperature.Collect(ch)
	c.fanSpeed.Collect(ch)
}

func main() {
	flag.Parse()

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	if err := gonvml.Initialize(); err != nil {
		log.Fatal().
			Err(err).
			Msgf("Couldn't initialize gonvml. Make sure NVML is in the shared library search path.")
	}

	if driverVersion, err := gonvml.SystemDriverVersion(); err != nil {
		log.Error().
			Err(err).
			Msg("Cannot get SystemDriverVersion()")
	} else {
		log.Info().Msgf("SystemDriverVersion(): %v", driverVersion)
	}

	prometheus.MustRegister(NewCollector())

	// Serve on all paths under addr
	log.Info().Msgf("Listening on %s", *addr)
	log.Error().
		Err(http.ListenAndServe(*addr, promhttp.Handler())).
		Msg("Shutting down")

	if err := gonvml.Shutdown(); err != nil {
		log.Error().
			Err(err).
			Msg("Failed to shutdown NVML")
	} else {
		log.Info().Msg("Shutting down NVML")
	}
}
