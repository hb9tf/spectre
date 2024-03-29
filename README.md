# Spectre

Spectre is an SDR based long term, wide spectrum collection and analysis tool.

![Waterfall image rendered from collected data.](https://github.com/hb9tf/spectre/blob/main/waterfall.jpg?raw=true)

## Collection

### Prerequisites

You will need a working setup for one of the [supported SDRs](#supported-sdrs).

Notes: This has primarily been tested on macOS 12.1 and Debian but it will probably work elsewhere as well.

### Flags

* `-lowFreq`: The lower frequency to start the sweeps with in Hz.

* `-highFreq`: The upper frequency to end the sweeps with in Hz.

* `-binSize`: The FFT bin width (frequency resolution) in Hz. BinSize is a maximum, smaller more convenient bins will be used.

* `-integrationInterval`: The duration during which to collect information per frequency.

    > Note: HackRF's `hackrf_sweep` is sweeping at much higher rates than e.g. RTL SDR's `rtl_power`
    > but on the flipside, it does not allow providing an integration interval. Thus this integration
    > is done in software which is more resource intense when using a HackRF.

* `discardOutOfRange`: When set to `true` (default) this causes samples to be filtered which are captured by the SDR but outside the specified range.

    > Note: This is useful to save bandwidth and storage when using an SDR like HackRF which returns samples in a
    > 20MHz bandwidth even when only a 2MHz sample range is needed.

* `-sdr`: Which SDR type to use (determines the CLI command which is called).

* `-identifier`: Unique identifier for the source instance (needs to be assigned).

* `-output`: Export mechanism to use, needs to be one of: `csv`, `sqlite`, `mysql`, `spectre`. See [Output section](#output) below.

    * For `sqlite` output option:
        * `sqliteFile`: File path of the sqlite DB file to use (default: `/tmp/spectre`). Note that the DB file is created if it doesn't already exist.
    * For `mysql` output option:
        * `mysqlServer`: MySQL TCP server endpoint to connect to (IP/DNS and port). Defaults to "127.0.0.1:3306".
        * `mysqlUser`: MySQL DB user
        * `mysqlPasswordFile`: Path to the file containing the password for the MySQL user.
        * `mysqlDBName`: Name of the DB to use. Defaults to `spectre`.
    * For `spectre` output option:
        *	`spectreServer`: URL scheme, address and port of the spectre server in the following format: "https://localhost:8443"
	    * `spectreServerSamples`: Defines how many samples should be sent to the server at once (default is 100).

We're using [glog]() which allows you to modify the logging behavior through flags as well if needed. The most useful ones:

* `logtostderr`: Logs to stderr instead of logfiles
* `v`: Shows all `V(x)` messages for `x` less or equal the value of this flag.

For more info on how to control logging, see the following:

* [Go glog](https://github.com/golang/glog)
* [glog](https://github.com/google/glog)

### Output

The following output options are currently supported, controlled via the `-output` flag:

* `csv`: CSV formatted export to `stdout`.
* `sqlite`: Write samples to local sqlite DB.
* `mysql`: Write samples to a MySQL DB.
* `spectre`: Write samples to a remote Spectre server endpoint.

Note: See additional control flags for each output option in the [Flags section](#flags) above.

Generally, the output contains the following data:
* Source: Source type (e.g. "hackrf" or "rtl_sdr").
* Identifier: Unique identifier for the specific instance as defined by the `-id` flag.
* Center Frequency: Center frequency of the sample (halfway between lower and upper frequency).
* Low Frequency: Lower frequency used for this sample's bin.
* High Frequency: Upper frequency used for this sample's bin.
* Start Time: Unix timestamp in milliseconds at which the measurement started.
* End Time: Unix timestamp in milliseconds at which the measurement ended.
* DB Low: Lowest signal strength measured across the samples aggregated in this frequency bucket.
* DB High: Highest signal strength measured across the samples aggregated in this frequency bucket.
* DB Avg: Average signal strength  across the samples aggregated in this frequency bucket.
* Sample Count: Number of measurements aggregated into this sample.

### Examples

#### Example 1

The following uses an RTL SDR to sweep from 400-500MHz with a bin size of 12.5kHz and 10s integration
per channel and writes the output to stdout as a CSV:

```
$ go run spectre.go -sdr rtlsdr -lowFreq 400000000 -highFreq 500000000 -binSize 12500 -integrationInterval 10s -output csv
Running RTL SDR sweep: "/opt/homebrew/bin/rtl_power -f 400000000:500000000:12500 -i 10s -"
...
489046189,489040764,489051614,1639222100000,1639222100000,-19.350000,-19.350000,-19.350000,160
489057039,489051614,489062464,1639222100000,1639222100000,-19.550000,-19.550000,-19.550000,160
489067889,489062464,489073314,1639222100000,1639222100000,-18.840000,-18.840000,-18.840000,160
489078739,489073314,489084164,1639222100000,1639222100000,-17.120000,-17.120000,-17.120000,160
489089589,489084164,489095014,1639222100000,1639222100000,-16.110000,-16.110000,-16.110000,160
...
```

#### Example 2

In this example, we use an RTL SDR to sweep from 400-500MHz with a bin size of 12.5kHz and 10s integration
per channel and write the output to a sqlite DB in `/tmp/spectre` (the file is created if it doesn't already exist):

```
$ go run spectre.go -sdr rtlsdr -lowFreq 400000000 -highFreq 500000000 -binSize 12500 -integrationInterval 10s -output sqlite -sqliteFile "/tmp/spectre"
Running RTL SDR sweep: "/opt/homebrew/bin/rtl_power -f 400000000:500000000:12500 -i 10s -"
Sample export counts: map[error:0 success:1000 total:1000]
Sample export counts: map[error:0 success:2000 total:2000]
Sample export counts: map[error:0 success:3000 total:3000]
...
```

### Supported SDRs

Currently there is support for:

* [RTL SDR](https://osmocom.org/projects/rtl-sdr/wiki/Rtl-sdr)

    Use the `-sdr rtlsdr` flag for Spectre.

    Ensure you installed the `rtl-sdr` tools - specifically `rtl_power` needs to be findable via `$PATH`.

    * macOS: `brew install librtlsdr`
    * Debian/Ubuntu: `apt-get install rtl-sdr`

    Note: RTL SDR support has been less tested than HackRF so there might be more rough edges here.

* [HackRF](https://greatscottgadgets.com/hackrf/)

    Use the `-sdr hackrf` flag for Spectre.

    Ensure you installed the `hackrf` tools - specifically `hackrf_sweep` needs to be findable via `$PATH`.

    * macOS: `brew install hackrf` or `sudo port install hackrf`
    * Debian/Ubuntu: `apt-get install hackrf`

    > Note: You might have to install the HackRF tools from source and update your HackRF's firmware if you
    > run into problems. We have confirmed this working with the latest
    > [HackRF source](https://github.com/greatscottgadgets/hackrf) as of 2022-01-15
    > ([commit `8660e44`](https://github.com/greatscottgadgets/hackrf/commit/8660e44575b401855ae75d25e439c0e785c1af04))
    > and [release `2021.03.1`](https://github.com/greatscottgadgets/hackrf/releases/tag/v2021.03.1) (e.g. firmware).

## Server

This is an optional piece of spectre which can centrally collect samples from one or more endpoints.

Note: This is experimental at the moment.

The server can be run as follows:

```
go run server.go -logtostderr -storage sqlite -sqliteFile /tmp/spectre
I0116 12:06:35.799644    1333 server.go:88] Resorting to serving HTTP because there was no certificate and key defined.
...
```

See `server.go` for more details such as available flags.

Once running, the server presents two endpoints:

* `/spectre/v1/collect`: The endpoint the collection binary uses to send its samples.
* `/spectre/v1/render`: An endpoint to call to get a rendered image back. Supported `GET` parameters are:

    * Filter options: 

        * `sdr`: Either `rtlsdr` or `hackrf`.
        * `identifier`: The identifier of a specific sender in order to just render samples for that one station.
        * `startFreq`: Lowest frequency to filter for.
        * `endFreq`: Highest frequency to filter for.
        * `startTime`: Unix start time in milliseconds in UTC.
        * `endTime`: Unix end time in milliseconds in UTC.

    * Image options:

        * `addGrid`: Whether to add a grid or not (default `1`). To disable either set it to `0` or `false`.
        * `imgWidth`: Desired image width in pixels.
        * `imgHeight`: Desired image height in pixels.
        * `imageType`: Either `jpg` (default) or `png`.

## Renderer

The renderer `render.go` can be used to render collected Spectre data as a waterfall.

Note: This is highly experimental at the moment.

The renderer currently only supports data collected into a sqlite DB and can be run as follows:

```
$ go run render.go -source sqlite -sqliteFile /tmp/spectre -sdr hackrf -imgPath /tmp/out.jpg
Selected source metadata:
  - Low frequency: 88.00 MHz
  - High frequency: 128.00 MHz
  - Start time: 2022-01-07T09:39:26 (1641544766)
  - End time: 2022-01-07T10:51:26 (1641549086)
  - Duration: 1h11m59.997s
Rendering image (3208 x 432)
  - Frequency resolution: 12.47 kHz per pixel
  - Time resultion: 10.00 seconds per pixel
Writing image to "/tmp/out.jpg"
```

See `render.go` for supported flags as there are more filter options than showed here.