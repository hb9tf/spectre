# Spectre

Spectre is an SDR based long term spectrum analysis tool.

## Flags

* `-lowFreq`: The lower frequency to start the sweeps with in Hz.

* `-highFreq`: The upper frequency to end the sweeps with in Hz.

* `-binSize`: The FFT bin width (frequency resolution) in Hz. BinSize is a maximum, smaller more convenient bins will be used.

* `-integrationInterval`: The duration during which to collect information per frequency.

    > Note: HackRF's `hackrf_sweep` is sweeping at much higher rates than e.g. RTL SDR's `rtl_power`
    > but on the flipside, it does not allow providing an integration interval. Thus this integration
    > is done in software which is more resource intense when using a HackRF.

* `-sdr`: Which SDR type to use (determines the CLI command which is called).

* `-id`: Unique identifier for the source instance (needs to be assigned).

* `output`: Export mechanism to use, needs to be one of: `csv`, `sqlite`, `elastic`, `datastore`. See [Output section](#output) below.

    * For `sqlite` output option:
        * `dbFile`: File path of the sqlite DB file to use (default: `/tmp/spectre`).

    * For `elastic` output option:
        * `esEndpoints`: Comma separated list of endpoints for elastic export (defaults to `http://localhost:9200/`).
	    * `esUser`: Username to use for elastic export (defaults to `elastic`).
	    * `esPwdFile`: File to read password for elastic export from.

	* For GCP based output options (e.g. `datastore`):
	    * `gcpProject`: GCP project.
        * `gcpSvcAcctKey`: Full path to a GCP Service accout key file (JSON).

## Output

The following output options are currently supported, controlled via the `-output` flag:

* `csv`: CSV formatted export to `stdout`.
* `elastic`: Experimental support to write to an Elastic backend.
* `datastore`: Experimental support to write to GCP Cloud Datastore.

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

## Example

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

## Supported SDRs

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

    * macOS: `brew install hackrf`
    * Debian/Ubuntu: `apt-get install hackrf`