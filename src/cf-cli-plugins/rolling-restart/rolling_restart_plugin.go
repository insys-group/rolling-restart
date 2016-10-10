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

//# of instances to restart at a time. It makes it fast when app has large number of instances running
var rollingInstanceCount int = 1
//Timeout in minutes when process wait for the instance to restart
var restartTimeoutMinutes int = 3
//Timeout in minutes for the instance restart initiated
const restartInitiateTimeout int= 2


type RollingRestartPlugin struct {
}

type AppStatus struct {
	countRunning   int
	countRequested int
	state          string
	appName        string
}

func (cmd *RollingRestartPlugin) Run(cliConnection plugin.CliConnection, args []string) {
	if len(args) == 1 {
		fmt.Println("APP_NAME is required.")
		os.Exit(1)
	}
	cmd.restartInstances(cliConnection, args)
}

func (cmd *RollingRestartPlugin) restartInstances(cliConnection plugin.CliConnection, args []string) {
	appName := args[1]
	for argsCount := 2; argsCount < len(args); argsCount++ {
		parsedArgs:=strings.Split(args[argsCount], "=")
		key := parsedArgs[0]
		value := parsedArgs[1]
		if strings.Compare(key, "rollingInstanceCount")==0 {
			if val,err := strconv.ParseInt(value, 10, 64); err != nil || val < 1 {
				fmt.Println("Parameter rollingInstanceCount should be a valid positive/non-zero integer")
				os.Exit(1)
			} else {
				rollingInstanceCount = int(val)
			}
		}
		if strings.Compare(key, "restartTimeoutMinutes")==0 {
			if val,err := strconv.ParseInt(value, 10, 64); err != nil || val < 1 {
				fmt.Println("Parameter restartTimeoutMinutes should be a valid positive/non-zero integer")
				os.Exit(1)
			} else {
				restartTimeoutMinutes = int(val)
			}
		}
	}
	fmt.Println("Rolling restart started...")
	appStatus, err := cmd.getAppStatus(cliConnection, appName)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Printf("Aplication \"%s\" current state is \"%s\" with requested instances \"%d\" and running instances \"%d\"",
		appStatus.appName,appStatus.state,appStatus.countRequested,appStatus.countRunning)
	
	if appStatus.countRequested != appStatus.countRunning || appStatus.state != "started" {
		fmt.Println("Application is not stable right now. Please try again later")
		os.Exit(1)
	}
	
	if rollingInstanceCount > appStatus.countRequested {
		fmt.Printf("Parameter rollingInstanceCount(%d) cannot be greater than total application instances(%d)\n",rollingInstanceCount,appStatus.countRequested)
		os.Exit(1)
	}
	
	//Initialize list of instances
	instances:=make([]int, appStatus.countRequested)
	for i:=0; i<appStatus.countRequested; i++ {
		instances[i]=i
	}
	//Restart all instances in groups
	//for instanceId := 0; instanceId < appStatus.countRequested; instanceId++ {
	for i:=0; i<appStatus.countRequested; i+=rollingInstanceCount {
		var rollingInstances []int
		if i+rollingInstanceCount >= appStatus.countRequested {
			rollingInstances=instances[i:]
		} else {
			rollingInstances=instances[i:i+rollingInstanceCount]
		}
		
		//Restart instances
		fmt.Print("\n\nRestarting instances ")
		fmt.Println(rollingInstances)
		fmt.Printf("----------------------\n")
		
		for _,instanceId := range rollingInstances {
			cliConnection.CliCommandWithoutTerminalOutput("restart-app-instance", appName, strconv.Itoa(instanceId))
		}
		
		//Wait for instance restart initiate. Timeout after 30 seconds
		if !cmd.waitForRestartInitiate(cliConnection, appName) {
			fmt.Printf("Instance restart could not be initiated in given time. Cannot continue. Exiting.")
			os.Exit(1)
		}
		
		//Wait for instance restart and running again. Timeout after 2 mins
		if !cmd.waitForRestartFinish(cliConnection, appName, rollingInstances) {
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
		appStatus, err := cmd.getAppStatus(cliConnection, appName)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if appStatus.countRequested != appStatus.countRunning && appStatus.state == "started" {
			restartInitiated = true
			break
		}
		
		retryCount++
		
		if retryCount > 12 * restartInitiateTimeout {
			ticker.Stop()
			break
		}
	}
	return restartInitiated
}

func (cmd *RollingRestartPlugin) waitForRestartFinish(cliConnection plugin.CliConnection, appName string, rollingInstances []int) (bool) {
	restartFinished := false
	ticker := time.NewTicker(5 * time.Second)
	retryCount := 0
	for _ = range ticker.C {
		output,err:=cliConnection.CliCommandWithoutTerminalOutput("app", appName)
		if err != nil {
			fmt.Println(err)
			ticker.Stop()
			os.Exit(1)
		}

		formattedOutput,runningCount := cmd.parseOutput(output, rollingInstances)
		fmt.Printf("%s",formattedOutput)
		
		appStatus, err := cmd.getAppStatus(cliConnection, appName)
		if err != nil {
			fmt.Println(err)
			ticker.Stop()
			os.Exit(1)
		}
		
		if appStatus.countRequested == appStatus.countRunning && appStatus.state == "started" && runningCount == len(rollingInstances) {
			restartFinished = true
			break
		}
		
		retryCount++ 
		
		if retryCount > 12 * restartTimeoutMinutes {
			ticker.Stop()
			break
		}
	}
	return restartFinished
}

func (cmd *RollingRestartPlugin) parseOutput(output []string, rollingInstances []int) (string, int) {
	var buffer bytes.Buffer
	runningCount := 0
	for _,data := range output {
		for _,instanceId := range rollingInstances {
			if strings.HasPrefix(data, "#"+strconv.Itoa(instanceId)) {
				buffer.WriteString(data)
				buffer.WriteString("\n")
				if strings.Contains(data, "running") {
					runningCount++
				}
			}
		}
	}
	return buffer.String(), runningCount
}

func (cmd *RollingRestartPlugin) getAppStatus(cliConnection plugin.CliConnection, appName string) (*AppStatus, error) {
	app, err := cliConnection.GetApp(appName)
	if err != nil {
		return nil, err
	}

	status := &AppStatus{
		appName: appName,
		countRunning:   0,
		countRequested: 0,
		state:          app.State,
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
				HelpText: "Restarts application by rolling the 1+ instances at a time so that application is available all the time during restart",
				UsageDetails: plugin.Usage{
					Usage: "   cf rolling-restart APP_NAME [rollingInstanceCount=n] [restartTimeoutMinutes=n]\n\n   APP_NAME is the Name of the application which needs restart\n   rollingInstanceCount is the # of instances to restart at a time. It helps rolling restart to finish quicker\n   restartTimeoutMinutes is the timeout value in minutes to wait for the instance to finish restart",
				},
			},
		},
	}
}

func main() {
	plugin.Start(new(RollingRestartPlugin))
}
