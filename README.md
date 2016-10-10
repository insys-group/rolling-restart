# rolling-restart
Rolling restart of Cloud Foundry application instances. It restarts instances one at a time. Currently macosx and windows binaries have been created in bin folder.

## Steps to install and run plugin in CF CLI
1. git clone https://github.com/insys-group/rolling-restart.git
2. cd rolling-restart
3. cf install-plugin ./bin/rolling_restart_plugin
4. cf rolling-restart APP_NAME

Happy Coding!!!!!!!!!
