package log

import (
	"os"

	"github.com/newrelic/go-agent/v3/integrations/logcontext-v2/nrlogrus"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/sirupsen/logrus"
)

var (
	InfoLogger  *logrus.Logger
	ErrorLogger *logrus.Logger
	NewRelicApp *newrelic.Application
)

func SetupLoggers() {
	var err error

	InfoLogger = logrus.New()
	ErrorLogger = logrus.New()

	NewRelicApp, err = newrelic.NewApplication(
		newrelic.ConfigAppName(os.Getenv("NEW_RELIC_APP_NAME")),
		newrelic.ConfigLicense(os.Getenv("NEW_RELIC_LICENSE_KEY")),
		newrelic.ConfigDistributedTracerEnabled(true),
	)

	if err != nil {
		logrus.Fatal(err)
	}

	nrlogrusFormatter := nrlogrus.NewFormatter(NewRelicApp, &logrus.TextFormatter{})

	// Set Logrus log level and formatter
	InfoLogger.SetLevel(logrus.InfoLevel)
	InfoLogger.SetFormatter(nrlogrusFormatter)

	ErrorLogger.SetLevel(logrus.ErrorLevel)
	ErrorLogger.SetFormatter(nrlogrusFormatter)
}
