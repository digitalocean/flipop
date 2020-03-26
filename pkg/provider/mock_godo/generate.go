package mock_godo

//go:generate mockgen -destination=godo.go github.com/digitalocean/godo DomainsService,FloatingIPsService,FloatingIPActionsService,DropletsService
