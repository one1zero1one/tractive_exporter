package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/mmcloughlin/geohash"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Key for unique geohash/tracker map
type uniqueGeoStates struct {
	tracker string
	geohash string
}

// Value for the unique geohash/tracker map
type uniqueGeoStatesValue struct {
	counter       int32
	lastTimestamp int64
}

// Unique geohash/tracker combo
var mapOfUniqueGeoStates map[uniqueGeoStates]uniqueGeoStatesValue

// Value for the map of tracker geo memory
type geoMemory struct {
	prevLat     float64
	prevLon     float64
	prevGeohash string
	lat         float64
	lon         float64
	geohash     string
	distance    float64
	updateTime  time.Time
	age         time.Duration
}

// Map of previous location (with tracker id as key)
var mapOfTrackerGeoMemory map[string]geoMemory

/*  the /info endpoint (@TODO)
{
    "name": "XXXX",
    "tracker_id": "XXXXXXXX",
    "image_url": "https://cdn.tractive.com/3/media/resource/XXXXXXXX.jpg",
    "owner_name": "XXXXX"
}
*/

// Info ...
type Info struct {
	Name      string
	TrackerID string
	ImageURL  string
	OwnerName string
}

/*  the /position endpoint
{
    "time": 1609533659,
    "lat": XX.XXXXXXX,
    "lon": XX.XXXXXXX,
    "speed": 0.2,
    "alt": 4,
	"lt_active": true
}
... or...
{
    "code": 3555,
    "category": "PUBLIC SHARE",
    "message": "The public share does not exist.",
    "detail": null
}
*/

// Position ...
type Position struct {
	Time    int64   `json:"time"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Speed   float64 `json:"speed"`
	Alt     int     `json:"alt"`
	Live    bool    `json:"lt_active"`
	Code    int     `json:"code"`
	Message string  `json:"message"`
}

var (

	// What to monitor
	trackersList = flag.String("trackers.list", "",
		"Comma separated list of IDs from the public URLs")

	// Http client
	tr = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client = &http.Client{Transport: tr}

	// Serve Metrics
	listenAddress = flag.String("web.port", ":9101",
		"Address to listen on for telemetry")
	metricsPath = flag.String("web.path", "/metrics",
		"Path under which to expose metrics")

	// Metrics Description
	up = prometheus.NewDesc(
		prometheus.BuildFQName("tractive", "", "up"),
		"Was the last Tractive query successful.",
		nil, nil,
	)

	lastReceivedTime = prometheus.NewDesc(
		prometheus.BuildFQName("tractive", "", "last_time"),
		"Timestamp of the last reported message",
		[]string{"tracker"}, nil,
	)

	lastReceivedAge = prometheus.NewDesc(
		prometheus.BuildFQName("tractive", "", "age"),
		"Age of the last reported message",
		[]string{"tracker"}, nil,
	)

	trackerLatitude = prometheus.NewDesc(
		prometheus.BuildFQName("tractive", "", "latitude"),
		"Latitude of the tracker",
		[]string{"tracker"}, nil,
	)

	trackerLongitude = prometheus.NewDesc(
		prometheus.BuildFQName("tractive", "", "longitude"),
		"Longitude of the tracker",
		[]string{"tracker"}, nil,
	)

	trackerGeohash = prometheus.NewDesc(
		prometheus.BuildFQName("tractive", "", "geohash_total"),
		"Geohash count",
		[]string{"tracker", "geohash"}, nil,
	)

	trackerDistance = prometheus.NewDesc(
		prometheus.BuildFQName("tractive", "", "distance"),
		"Distance from last location",
		[]string{"tracker"}, nil,
	)

	trackerDistanceAge = prometheus.NewDesc(
		prometheus.BuildFQName("tractive", "", "distance_time"),
		"Time in which the distance from last location was done",
		[]string{"tracker"}, nil,
	)
	trackerSpeed = prometheus.NewDesc(
		prometheus.BuildFQName("tractive", "", "speed"),
		"Speed of the tracker",
		[]string{"tracker"}, nil,
	)

	trackerAltitude = prometheus.NewDesc(
		prometheus.BuildFQName("tractive", "", "altitude"),
		"Altitude of the tracker",
		[]string{"tracker"}, nil,
	)

	trackerIsLive = prometheus.NewDesc(
		prometheus.BuildFQName("tractive", "", "live"),
		"Is tracker live",
		[]string{"tracker"}, nil,
	)
	apiIsPissed = prometheus.NewDesc(
		prometheus.BuildFQName("tractive", "", "code"),
		"API response code",
		[]string{"tracker"}, nil,
	)

	// one day I'll have to learn how to properly scope vars
	newLocation bool
	uniqueGeo   uniqueGeoStatesValue
)

// Custom exporters require 4 stubs

// Exporter ...
type Exporter struct {
	shareList             []string
	mapOfUniqueGeoStates  map[uniqueGeoStates]uniqueGeoStatesValue
	mapOfTrackerGeoMemory map[string]geoMemory
}

// NewExporter ...
func NewExporter(shareList []string,
	mapOfUniqueGeoStates map[uniqueGeoStates]uniqueGeoStatesValue,
	mapOfTrackerGeoMemory map[string]geoMemory) *Exporter {
	return &Exporter{
		shareList:             shareList,
		mapOfUniqueGeoStates:  mapOfUniqueGeoStates,
		mapOfTrackerGeoMemory: mapOfTrackerGeoMemory,
	}
}

// Describe ...
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- up
	ch <- lastReceivedTime
	ch <- lastReceivedAge
	ch <- trackerLatitude
	ch <- trackerLongitude
	ch <- trackerGeohash
	ch <- trackerDistance
	ch <- trackerDistanceAge
	ch <- trackerSpeed
	ch <- trackerAltitude
	ch <- trackerIsLive
	ch <- apiIsPissed
}

// Collect ...
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {

	//Can we reach the endpoint at all?
	timeout := 1 * time.Second
	_, err := net.DialTimeout("tcp", "graph.tractive.com:443", timeout)
	if err != nil {
		ch <- prometheus.MustNewConstMetric(
			up, prometheus.GaugeValue, 0,
		)
		log.Println(err)
		return
	}
	ch <- prometheus.MustNewConstMetric(
		up, prometheus.GaugeValue, 1,
	)

	//Go get'em
	e.HitTractiveApisAndUpdateMetrics(ch)
}

// HitTractiveApisAndUpdateMetrics ...
func (e *Exporter) HitTractiveApisAndUpdateMetrics(ch chan<- prometheus.Metric) {

	// For each tracker
	for _, id := range e.shareList {

		// Compose url
		url := "https://graph.tractive.com/3/public_share/" + id + "/position"

		// Compose request
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			log.Fatal(err)
		}

		// Be civilized
		req.Header.Set("User-Agent", "tractive_prometheus_exporter")

		// Make request
		resp, err := client.Do(req)
		if err != nil {
			log.Fatal(err)
		}

		// Close when done (might not be ideal with
		//					the loop, but ¯\_(ツ)_/¯)
		if req.Body != nil {
			defer req.Body.Close()
		}

		// Read and print if debug is on
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Fatal(err)
		}
		log.Println(string(body))

		// New variable to unmarshal to
		p := new(Position)

		// Unmarshal response
		err = json.Unmarshal(body, &p)
		if err != nil {
			log.Println("Unmarshall error", err)
		}

		log.Println(nicePrint(p))

		// expose them metrics ONLY when api doesn't throw a tantrum
		if p.Code == 0 {

			// last reported measurement's timestamp
			ch <- prometheus.MustNewConstMetric(
				lastReceivedTime, prometheus.GaugeValue, float64(p.Time), id,
			)

			// age is duration from the last received timestamp
			age := time.Now().Unix() - p.Time
			ch <- prometheus.MustNewConstMetric(
				lastReceivedAge, prometheus.GaugeValue, float64(age), id,
			)

			// lat and long (not necesarily useful to be sent as metrics, but there they are)
			ch <- prometheus.MustNewConstMetric(
				trackerLatitude, prometheus.GaugeValue, p.Lat, id,
			)
			ch <- prometheus.MustNewConstMetric(
				trackerLongitude, prometheus.GaugeValue, p.Lon, id,
			)

			// geohash is a much better fit for sending as context
			encoded := geohash.Encode(p.Lat, p.Lon)

			// if different geohash, update state and compute distance and age.
			newLocation = false
			if encoded != e.mapOfTrackerGeoMemory[id].geohash {
				newLocation = true
				e.mapOfTrackerGeoMemory[id] = geoMemory{
					prevLat:     e.mapOfTrackerGeoMemory[id].lat,
					prevLon:     e.mapOfTrackerGeoMemory[id].lon,
					prevGeohash: e.mapOfTrackerGeoMemory[id].geohash,
					lat:         p.Lat,
					lon:         p.Lon,
					geohash:     encoded,
					distance: Distance(
						e.mapOfTrackerGeoMemory[id].lat,
						e.mapOfTrackerGeoMemory[id].lon,
						p.Lat,
						p.Lon),
					updateTime: time.Now(),
					age:        time.Now().Sub(e.mapOfTrackerGeoMemory[id].updateTime),
				}
				ch <- prometheus.MustNewConstMetric(
					trackerDistance, prometheus.GaugeValue, float64(e.mapOfTrackerGeoMemory[id].distance), id,
				)
				ch <- prometheus.MustNewConstMetric(
					trackerDistanceAge, prometheus.GaugeValue, float64(e.mapOfTrackerGeoMemory[id].age), id,
				)

			}

			// geohash as metric label for a counter when
			// (new geohashes) or (same geohashes but new timestamps)
			uniqueGeo = e.mapOfUniqueGeoStates[uniqueGeoStates{tracker: id, geohash: encoded}]
			if (uniqueGeo.lastTimestamp != p.Time) || (newLocation) {
				uniqueGeo = uniqueGeoStatesValue{
					counter:       uniqueGeo.counter + 1,
					lastTimestamp: p.Time,
				}
				ch <- prometheus.MustNewConstMetric(
					trackerGeohash, prometheus.CounterValue, float64(uniqueGeo.counter), id, encoded,
				)
			}

			ch <- prometheus.MustNewConstMetric(
				trackerSpeed, prometheus.GaugeValue, p.Speed, id,
			)
			ch <- prometheus.MustNewConstMetric(
				trackerAltitude, prometheus.GaugeValue, float64(p.Alt), id,
			)

			// bool to float64, we do what we must because we can
			var isLiveNumber float64
			if p.Live {
				isLiveNumber = 1
			}

			ch <- prometheus.MustNewConstMetric(
				trackerIsLive, prometheus.GaugeValue, isLiveNumber, id,
			)
		} else {
			ch <- prometheus.MustNewConstMetric(
				apiIsPissed, prometheus.GaugeValue, float64(p.Code), id,
			)
		}

	}
}

func hsin(theta float64) float64 {
	return math.Pow(math.Sin(theta/2), 2)
}

// Distance ... https://gist.github.com/cdipaolo/d3f8db3848278b49db68
func Distance(lat1, lon1, lat2, lon2 float64) float64 {
	var la1, lo1, la2, lo2, r float64
	la1 = lat1 * math.Pi / 180
	lo1 = lon1 * math.Pi / 180
	la2 = lat2 * math.Pi / 180
	lo2 = lon2 * math.Pi / 180
	r = 6378100 // Earth radius in METERS
	h := hsin(la2-la1) + math.Cos(la1)*math.Cos(la2)*hsin(lo2-lo1)
	return 2 * r * math.Asin(math.Sqrt(h))
}

func nicePrint(i interface{}) string {
	s, _ := json.Marshal(i)
	return string(s)
}

func prettyPrint(i interface{}) string {
	s, _ := json.MarshalIndent(i, "", "\t")
	return string(s)
}

// delteEmpty ... https://dabase.com/e/15006/
func deleteEmpty(s []string) []string {
	var r []string
	for _, str := range s {
		if str != "" {
			r = append(r, str)
		}
	}
	return r
}

func main() {

	// maps used to keep state of things will be passed to exporter
	mapOfUniqueGeoStates := make(map[uniqueGeoStates]uniqueGeoStatesValue)
	mapOfTrackerGeoMemory := make(map[string]geoMemory)

	// deal with params
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file, assume env variables are set.")
	}

	flag.Parse()

	// list of trackers from env and params
	shareList := deleteEmpty(
		append(strings.Split(os.Getenv("TRACTIVE_PUBLIC_SHARES"), ","), strings.Split(*trackersList, ",")...))

	exporter := NewExporter(shareList, mapOfUniqueGeoStates,
		mapOfTrackerGeoMemory)

	prometheus.MustRegister(exporter)

	http.Handle(*metricsPath, promhttp.Handler())

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
             <head><title>Tractive Exporter</title></head>
             <body>
             <h1>Tractive Tracker Data Exporter</h1>
             <p><a href='` + *metricsPath + `'>Metrics</a></p>
             </body>
             </html>`))
	})
	log.Fatal(http.ListenAndServe(*listenAddress, nil))

	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(":9101", nil))
}
