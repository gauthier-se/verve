# Changelog

## [0.2.0](https://github.com/gauthier-se/verve/compare/v0.1.0...v0.2.0) (2026-11-06)


### Features

* **api:** add the timezone preference ([#169](https://github.com/gauthier-se/verve/issues/169)) ([dcc460b](https://github.com/gauthier-se/verve/commit/dcc460b024dfc47a4cbebb54c00f1107118be567))
* **api:** list failed jobs for admins ([#163](https://github.com/gauthier-se/verve/issues/163)) ([5b2e883](https://github.com/gauthier-se/verve/commit/5b2e88373d0f1e06e1ef92b1326596cd21ab3a8b))
* **audit:** aggregate bursts of audit events ([#148](https://github.com/gauthier-se/verve/issues/148)) ([efbe6d4](https://github.com/gauthier-se/verve/commit/efbe6d436706ad4a40e37c007564f3bd168b9250))
* **audit:** purge old audit events ([#150](https://github.com/gauthier-se/verve/issues/150)) ([abff55a](https://github.com/gauthier-se/verve/commit/abff55a7a76a0c607bc2e77ab1a26e625da4379c))
* **audit:** read audit events through the API ([#161](https://github.com/gauthier-se/verve/issues/161)) ([20668a6](https://github.com/gauthier-se/verve/commit/20668a64ded44eef8441070a38edd080506c207e))
* **audit:** record audit events ([#140](https://github.com/gauthier-se/verve/issues/140)) ([e1933ca](https://github.com/gauthier-se/verve/commit/e1933ca1bc47b15310ac17a765e8666b7a81bf0b))
* **auth:** add login and logout with rate limiting ([#162](https://github.com/gauthier-se/verve/issues/162)) ([285d1b9](https://github.com/gauthier-se/verve/commit/285d1b97bd27737b1a9a526ae74793832f5ac9cc))
* **auth:** add server-side sessions ([#152](https://github.com/gauthier-se/verve/issues/152)) ([bfc9844](https://github.com/gauthier-se/verve/commit/bfc9844ebff8f9aaef65475a50d70d71a03a432b))
* **auth:** add users and password hashing ([#141](https://github.com/gauthier-se/verve/issues/141)) ([9e0511b](https://github.com/gauthier-se/verve/commit/9e0511b408b6752298981f938834a0f0343a46ab))
* **auth:** authenticate session cookies and enforce CSRF checks ([#154](https://github.com/gauthier-se/verve/issues/154)) ([0666811](https://github.com/gauthier-se/verve/commit/06668116048579f79ef8debbabe7ea17d441a4fb))
* **auth:** create the first admin from the setup token or the CLI ([#164](https://github.com/gauthier-se/verve/issues/164)) ([6710ef6](https://github.com/gauthier-se/verve/commit/6710ef676ab13ac4c540b8ba987e9874ae47b9e5))
* **config:** load or generate the secret key ([#146](https://github.com/gauthier-se/verve/issues/146)) ([d34d9a9](https://github.com/gauthier-se/verve/commit/d34d9a991e52db1c19667ef4b3e25fe44cd32a1a))
* **jobs:** add the jobs table and repository ([#129](https://github.com/gauthier-se/verve/issues/129)) ([170c7bf](https://github.com/gauthier-se/verve/commit/170c7bf2d34a62ff53835e7fe43a2670f0809926))
* **jobs:** retry, cancel and cleanly stop jobs ([#137](https://github.com/gauthier-se/verve/issues/137)) ([77146e1](https://github.com/gauthier-se/verve/commit/77146e10574ac93be352dc11ef17b41358d5f7d2))
* **jobs:** run a job in-process under its serial key ([#149](https://github.com/gauthier-se/verve/issues/149)) ([ea08904](https://github.com/gauthier-se/verve/commit/ea08904a185ee90b8a3ef4aaa3604d6787789a71))
* **jobs:** run jobs with a worker pool, leases and heartbeats ([#136](https://github.com/gauthier-se/verve/issues/136)) ([8e2f633](https://github.com/gauthier-se/verve/commit/8e2f633abde3b6fa4956ac600918f98225163254))
* **jobs:** schedule recurring jobs ([#147](https://github.com/gauthier-se/verve/issues/147)) ([61bd92d](https://github.com/gauthier-se/verve/commit/61bd92d128cf23a40ae0676def9e75d163431627))
* **store:** open the database next to a running server ([#132](https://github.com/gauthier-se/verve/issues/132)) ([591f573](https://github.com/gauthier-se/verve/commit/591f5738d4d430d02c38077fc1123811b868134f))
* **web:** build the authenticated shell and the login page ([#166](https://github.com/gauthier-se/verve/issues/166)) ([e955a77](https://github.com/gauthier-se/verve/commit/e955a77a82bab91680f6b39d101f2e138bffdf40))
* **web:** build the setup wizard ([#171](https://github.com/gauthier-se/verve/issues/171)) ([1e60c23](https://github.com/gauthier-se/verve/commit/1e60c23f4272b2d1f1624142578b54671fdcc5e1))


### Bug Fixes

* **auth:** answer 409 to a setup overtaken by another one ([#170](https://github.com/gauthier-se/verve/issues/170)) ([0c43e04](https://github.com/gauthier-se/verve/commit/0c43e0418b13e13507db581b025fead1a418f373))
* **jobs:** log the jobs that a claim abandons ([#160](https://github.com/gauthier-se/verve/issues/160)) ([bdfa3d5](https://github.com/gauthier-se/verve/commit/bdfa3d5dd06eefd3c69f3d715accb4a386d7651a))
* **store:** escape URI characters in the database path ([#153](https://github.com/gauthier-se/verve/issues/153)) ([5aeb1ff](https://github.com/gauthier-se/verve/commit/5aeb1ff6835c4e91973caec72648ebe755528224))
* **store:** stop waiting for a blocked checkpoint on shutdown ([#133](https://github.com/gauthier-se/verve/issues/133)) ([38ed54e](https://github.com/gauthier-se/verve/commit/38ed54eb29f0edb5154901716c06b17bdf49ad88))

## 0.1.0 (2026-10-30)


### Features

* **api:** add the huma api skeleton, meta endpoint and typed client ([#64](https://github.com/gauthier-se/verve/issues/64)) ([e694d3d](https://github.com/gauthier-se/verve/commit/e694d3deb2f0d3707b191e1b1f056ea321261e5a))
* **cli:** bootstrap go module, cli root, clock and taskfile ([#36](https://github.com/gauthier-se/verve/issues/36)) ([c480f97](https://github.com/gauthier-se/verve/commit/c480f97fd44e95cf8b6107d5020ad5e44e2cbaf3))
* **config:** load and validate configuration from defaults, yaml and environment ([#46](https://github.com/gauthier-se/verve/issues/46)) ([9e56544](https://github.com/gauthier-se/verve/commit/9e56544f30abe067d2061a132f99b05ac516b5d5))
* **i18n:** set up i18next with english and french ([#47](https://github.com/gauthier-se/verve/issues/47)) ([c0806c4](https://github.com/gauthier-se/verve/commit/c0806c4c693e6f1ab12e86e4df8a68aa66b08b92))
* **obs:** add health and readiness endpoints and verve healthcheck ([#57](https://github.com/gauthier-se/verve/issues/57)) ([a2409f2](https://github.com/gauthier-se/verve/commit/a2409f21acecbb04a3166e009775cc18993b30dd))
* **obs:** add structured logging, request logging and panic recovery ([#56](https://github.com/gauthier-se/verve/issues/56)) ([5371cca](https://github.com/gauthier-se/verve/commit/5371ccafa3c6c3ef4dd2d10058e8e376f11772d0))
* **server:** add security headers middleware ([#65](https://github.com/gauthier-se/verve/issues/65)) ([6c77637](https://github.com/gauthier-se/verve/commit/6c77637939943e1c9e1e6ec44369972803e4a640))
* **server:** resolve client ip and scheme behind trusted proxies ([#50](https://github.com/gauthier-se/verve/issues/50)) ([a9656be](https://github.com/gauthier-se/verve/commit/a9656be46094473da3fc8b0503b30d43bc150590))
* **server:** serve http with graceful shutdown and a data directory lock ([#48](https://github.com/gauthier-se/verve/issues/48)) ([9e4fa8d](https://github.com/gauthier-se/verve/commit/9e4fa8d5ff842e24bf3a661d132f4184f4978427))
* **server:** serve the embedded spa with client-side routing fallback ([#63](https://github.com/gauthier-se/verve/issues/63)) ([65615ec](https://github.com/gauthier-se/verve/commit/65615ec4afc103071bcad962a038513abee8a7b8))
* **store:** add storage interfaces, unit of work and instance settings ([#54](https://github.com/gauthier-se/verve/issues/54)) ([8064af6](https://github.com/gauthier-se/verve/commit/8064af68911fd72a3a1cc418676e82f482df7d6c))
* **store:** open sqlite with wal and separate writer and reader pools ([#52](https://github.com/gauthier-se/verve/issues/52)) ([4a95886](https://github.com/gauthier-se/verve/commit/4a95886ca0bc206120ef28ebebf89f8676551777))
* **store:** run embedded migrations at startup and with verve migrate ([#53](https://github.com/gauthier-se/verve/issues/53)) ([e9dc678](https://github.com/gauthier-se/verve/commit/e9dc67805a3253412cea92c4171e3208da1dd588))
* **web:** bootstrap vite app with tanstack router and query ([#40](https://github.com/gauthier-se/verve/issues/40)) ([38ea8dd](https://github.com/gauthier-se/verve/commit/38ea8ddd717a40a4993f2a243e49524964c28ded))
* **web:** build the spa shell with version footer and connection screens ([#66](https://github.com/gauthier-se/verve/issues/66)) ([b96dad6](https://github.com/gauthier-se/verve/commit/b96dad61a911807b1eab3c00e3f62aac8db110f9))


### Bug Fixes

* **i18n:** always regenerate translation types ([#68](https://github.com/gauthier-se/verve/issues/68)) ([70caf74](https://github.com/gauthier-se/verve/commit/70caf740da0311706fef4cb72e7f393f83a389d3))
