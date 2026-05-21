package handlers

import "github.com/vmkit-dev/vmkit-agent/internal/daemon"

func RegisterAll(d *daemon.Daemon) {
	d.RegisterHandler("health.check", HealthCheck)
	d.RegisterHandler("vm.harden", VMHarden)
	d.RegisterHandler("vm.openport", VMOpenPort)
	d.RegisterHandler("vm.upgrade", VMUpgrade)
	d.RegisterHandler("vm.diagnose", VMDiagnose)
	d.RegisterHandler("kamal.deploy", KamalDeploy)
	d.RegisterHandler("tls.issue", TLSIssue)
	d.RegisterHandler("creds.rotate", CredsRotate)
	d.RegisterHandler("logs.fetch", LogsFetch)
}
