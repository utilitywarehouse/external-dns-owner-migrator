# external-dns-owner-migrator

This is tooling to help us migrate external-DNS records to a new owner.

It can be used to migrate existing Ingress resources to a new owner id and/or
delete DNS records marked with an owner id.

At the moment it only supports AWS Route53 DNS zones.

Example usage:
```
$ ./external-dns-owner-migrator -migrate -aws-zone-id=ZPLLMOCKBH0LL -kube-context=exp-1-aws -external-dns-prefix=infra -external-dns-owner-id-old=infra -external-dns-owner-id-new=exp-1
-aws
```
