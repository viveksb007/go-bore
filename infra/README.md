This `infra` folder contains files needed to containerize the `gobore` application and deploy it on K8s. 

It seems like a lot of work to get this use-case running in K8s, initially I was thinking to expose control port 7835 on the pod which will be behind public facing load balancer. But we will also need someway to expose other ephemeral ports which `gobore clients` will receive when server allocates them, so we probably need a controller which integrates well with `gobore server` and AWS NLB APIs. Anyways I wasn't seeing successful control connection establishment itself, probably misconfiguring something in b/w.

