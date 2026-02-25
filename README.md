# LoRaWAN Platform Connector

Architecture diagram:

![Architecture diagram](docs/architecture.drawio.png)

## TODOs

- Automatic provisioning for existing/missed users on startup (and periodic sync)
- Deletion of tenants on user removal
- Gateway sync
- Device Type (profile) sync
- Device sync
- Event forwarding to kafka
- Request forwarding

## Gotchas

- Chirpstack expects the email address to be stable. Changes to a keycloak email address must therefore be manually corrected by an admin in chirpstack. Otherwise a second user and tenant will be created and the user will lose access to the previous tenant. The previous tenant will be deleted automatically!
