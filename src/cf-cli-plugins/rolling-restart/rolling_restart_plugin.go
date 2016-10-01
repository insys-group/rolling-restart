package main

import (
	"fmt"
	"strings"
	"os"
	"time"
	"strconv"
	"bytes"
	"code.cloudfoundry.org/cli/plugin"
)

type RollingRestartPlugin struct {
}

type AppStatus struct {
	countRunning   int
	countRequested int
	state          string
	routes         []string
	appName        string
}

func (cmd *RollingRestartPlugin) Run(cliConnection plugin.CliConnection, args []string) {
	if len(args) == 1 {
		fmt.Println("APP_NAME is required.")
		os.Exit(1)
	}
	cmd.restartInstances(cliConnection, args[1])
}

func (cmd *RollingRestartPlugin) restartInstances(cliConnection plugin.CliConnection, appName string) {
	fmt.Println("Rolling restart started")
	appStatus, _ := cmd.getAppStatus(cliConnection, appName)
	
	fmt.Printf("Aplication %s state is %s with requested instances %d and running instances %d",
		appStatus.appName,appStatus.state,appStatus.countRequested,appStatus.countRunning)
	
	if appStatus.countRequested != appStatus.countRunning || appStatus.state != "started" {
		fmt.Printf("Application is not stable right now. Please try again later")
		os.Exit(0)
	}
	
	//Restart all instances one after another
	for instanceId := 0; instanceId < appStatus.countRequested; instanceId++ {
		fmt.Printf("\nRestarting Instance #%d\n",instanceId)
		fmt.Printf("----------------------\n")
		
		//Restart instance
		cliConnection.CliCommandWithoutTerminalOutput("restart-app-instance", appName, strconv.Itoa(instanceId))
		
		//Wait for instance restart initiate. Timeout after 30 seconds
		if !cmd.waitForRestartInitiate(cliConnection, appName) {
			fmt.Printf("Instance restart could not be initiated in given time. Cannot continue. Exiting.")
			os.Exit(1)
		}
		
		//Wait for instance restart and running again. Timeout after 2 mins
		if !cmd.waitForRestartFinish(cliConnection, appName, instanceId) {
			fmt.Printf("Instance restart could not be finished in given time. Cannot continue. Exiting.")
			os.Exit(1)
		}
	}	
}

func (cmd *RollingRestartPlugin) waitForRestartInitiate(cliConnection plugin.CliConnection, appName string) (bool) {
	restartInitiated := false
	ticker := time.NewTicker(5 * time.Second)
	retryCount := 0
	for _ = range ticker.C {
		appStatus, _ := cmd.getAppStatus(cliConnection, appName)
	
		if appStatus.countRequested != appStatus.countRunning && appStatus.state == "started" {
			restartInitiated = true
			break
		}
		
		retryCount++
		
		if retryCount > 5 {
			ticker.Stop()
			break
		}
	}
	return restartInitiated
}

func (cmd *RollingRestartPlugin) waitForRestartFinish(cliConnection plugin.CliConnection, appName string, instanceId int) (bool) {
	restartFinished := false
	ticker := time.NewTicker(5 * time.Second)
	retryCount := 0
	for _ = range ticker.C {
		output,_:=cliConnection.CliCommandWithoutTerminalOutput("app", appName)
		formattedOutput,running := cmd.parseOutput(output, instanceId)
		fmt.Printf("%s",formattedOutput)

		appStatus, _ := cmd.getAppStatus(cliConnection, appName)
		if appStatus.countRequested == appStatus.countRunning && appStatus.state == "started" && running == true {
			restartFinished = true
			break
		}
		
		retryCount++ 
		
		if retryCount > 24 {
			ticker.Stop()
			break
		}
	}
	return restartFinished
}

func (cmd *RollingRestartPlugin) parseOutput(output []string, instanceId int) (string, bool) {
	var buffer bytes.Buffer
	running := false
	for _,data := range output {
		if strings.HasPrefix(data, "#"+strconv.Itoa(instanceId)) {
			buffer.WriteString(data)
			buffer.WriteString("\n")
			if strings.Contains(data, "running") {
				running=true
			}
		}
	}
	return buffer.String(), running
}

func (cmd *RollingRestartPlugin) getAppStatus(cliConnection plugin.CliConnection, appName string) (*AppStatus, error) {
	app, err := cliConnection.GetApp(appName)
	if nil != err {
		return nil, err
	}

	status := &AppStatus{
		appName: appName,
		countRunning:   0,
		countRequested: 0,
		state:          app.State,
		routes:         make([]string, len(app.Routes)),
	}

	if app.State != "stopped" {
		status.countRequested = app.InstanceCount
	}
	status.countRunning = app.RunningInstances
	return status, err
}

func (c *RollingRestartPlugin) GetMetadata() plugin.PluginMetadata {
	return plugin.PluginMetadata{
		Name: "rolling-restart",
		Version: plugin.VersionType{
			Major: 1,
			Minor: 0,
			Build: 0,
		},
		MinCliVersion: plugin.VersionType{
			Major: 6,
			Minor: 7,
			Build: 0,
		},
		Commands: []plugin.Command{
			{
				Name:     "rolling-restart",
				HelpText: "Restarts application by rolling the instances individually so that application is available even when its restarting",
				UsageDetails: plugin.Usage{
					Usage: "   rolling-restart\n   cf rolling-restart APP_NAME [INSTANCE_COUNT]\n   APP_NAME Name of the application which needs restart\n   INSTANCE_COUNT # of instances to restart. It helps rolling restart to finish quicker",
				},
			},
		},
	}
}

func main() {
	plugin.Start(new(RollingRestartPlugin))
}
