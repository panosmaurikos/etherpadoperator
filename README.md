controlplane $ cat etherpad-config.yaml 
apiVersion: v1
kind: ConfigMap
metadata:
  name: etherpad-settings
data:
  settings.json: |
    {
      "title": "Etherpad",
      "favicon": "favicon.ico",
      "skinName": "colibris",
      "dbType": "mysql",
      "dbSettings": {
        "host": "mysql-svc",
        "user": "etherpad_user",
        "password": "etherpad_password",
        "database": "etherpad_db",
        "charset": "utf8mb4"
      },
      "defaultPadText": "Welcome to Etherpad!\n",
      "padOptions": {
        "noColors": false,
        "showControls": true,
        "showChat": true,
        "showLineNumbers": true,
        "useMonospaceFont": false,
        "userName": false,
        "userColor": false,
        "rtl": false,
        "alwaysShowChat": false,
        "chatAndUsers": false,
        "lang": "en-gb"
      }
    }
controlplane $ cat etherpad-pvc.yaml 
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: etherpad-pvc
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
controlplane $      
