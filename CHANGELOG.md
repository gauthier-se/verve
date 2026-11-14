# Changelog

## [0.2.0](https://github.com/gauthier-se/verve/compare/v0.1.0...v0.2.0) (2026-11-14)


### Features

* **api:** add the timezone preference ([#169](https://github.com/gauthier-se/verve/issues/169)) ([dcc460b](https://github.com/gauthier-se/verve/commit/dcc460b024dfc47a4cbebb54c00f1107118be567))
* **api:** list and edit sources and their priorities ([#258](https://github.com/gauthier-se/verve/issues/258)) ([cae6707](https://github.com/gauthier-se/verve/commit/cae6707897354a4b644a67c54b9799a8a86c98fc))
* **api:** list failed jobs for admins ([#163](https://github.com/gauthier-se/verve/issues/163)) ([5b2e883](https://github.com/gauthier-se/verve/commit/5b2e88373d0f1e06e1ef92b1326596cd21ab3a8b))
* **api:** read a metric as a series ([#254](https://github.com/gauthier-se/verve/issues/254)) ([53b7f3a](https://github.com/gauthier-se/verve/commit/53b7f3afbda97f1a8bbccb5d1050f4ca685f4d1c))
* **api:** read the raw samples of a short range ([#257](https://github.com/gauthier-se/verve/issues/257)) ([01a2208](https://github.com/gauthier-se/verve/commit/01a2208d821c9d01d804e42056b5f9ce44df8968))
* **audit:** aggregate bursts of audit events ([#148](https://github.com/gauthier-se/verve/issues/148)) ([efbe6d4](https://github.com/gauthier-se/verve/commit/efbe6d436706ad4a40e37c007564f3bd168b9250))
* **audit:** purge old audit events ([#150](https://github.com/gauthier-se/verve/issues/150)) ([abff55a](https://github.com/gauthier-se/verve/commit/abff55a7a76a0c607bc2e77ab1a26e625da4379c))
* **audit:** read audit events through the API ([#161](https://github.com/gauthier-se/verve/issues/161)) ([20668a6](https://github.com/gauthier-se/verve/commit/20668a64ded44eef8441070a38edd080506c207e))
* **audit:** record audit events ([#140](https://github.com/gauthier-se/verve/issues/140)) ([e1933ca](https://github.com/gauthier-se/verve/commit/e1933ca1bc47b15310ac17a765e8666b7a81bf0b))
* **auth:** add API tokens and their repository ([#192](https://github.com/gauthier-se/verve/issues/192)) ([0b42392](https://github.com/gauthier-se/verve/commit/0b423926ca02763b875ed8b545c22a38fb074cd5))
* **auth:** add login and logout with rate limiting ([#162](https://github.com/gauthier-se/verve/issues/162)) ([285d1b9](https://github.com/gauthier-se/verve/commit/285d1b97bd27737b1a9a526ae74793832f5ac9cc))
* **auth:** add server-side sessions ([#152](https://github.com/gauthier-se/verve/issues/152)) ([bfc9844](https://github.com/gauthier-se/verve/commit/bfc9844ebff8f9aaef65475a50d70d71a03a432b))
* **auth:** add users and password hashing ([#141](https://github.com/gauthier-se/verve/issues/141)) ([9e0511b](https://github.com/gauthier-se/verve/commit/9e0511b408b6752298981f938834a0f0343a46ab))
* **auth:** authenticate requests with Bearer tokens and scopes ([#193](https://github.com/gauthier-se/verve/issues/193)) ([4346af4](https://github.com/gauthier-se/verve/commit/4346af4b113deb400a1b3084505725b91d42d19e))
* **auth:** authenticate session cookies and enforce CSRF checks ([#154](https://github.com/gauthier-se/verve/issues/154)) ([0666811](https://github.com/gauthier-se/verve/commit/06668116048579f79ef8debbabe7ea17d441a4fb))
* **auth:** create the first admin from the setup token or the CLI ([#164](https://github.com/gauthier-se/verve/issues/164)) ([6710ef6](https://github.com/gauthier-se/verve/commit/6710ef676ab13ac4c540b8ba987e9874ae47b9e5))
* **auth:** delete accounts ([#182](https://github.com/gauthier-se/verve/issues/182)) ([6bd2769](https://github.com/gauthier-se/verve/commit/6bd2769355ea4ab4b0e9d2933b418b7d2a1be355))
* **auth:** generate password reset links ([#175](https://github.com/gauthier-se/verve/issues/175)) ([d543262](https://github.com/gauthier-se/verve/commit/d5432621a8c77e968b13f6d28badc66e5953c2f4))
* **auth:** invite users with a single-use link ([#173](https://github.com/gauthier-se/verve/issues/173)) ([5d6ee49](https://github.com/gauthier-se/verve/commit/5d6ee49b6daf57439903ba2ee07564afd6c290e8))
* **auth:** manage accounts as an admin ([#174](https://github.com/gauthier-se/verve/issues/174)) ([4c17376](https://github.com/gauthier-se/verve/commit/4c173767ca688f8b8c0f544ed55b452560d048fd))
* **auth:** manage passwords and sessions through the API ([#172](https://github.com/gauthier-se/verve/issues/172)) ([fd3384c](https://github.com/gauthier-se/verve/commit/fd3384cf4979459c214c498ff31d7e6f55925e6c))
* **auth:** manage tokens through the API with rate limiting ([#194](https://github.com/gauthier-se/verve/issues/194)) ([7420be4](https://github.com/gauthier-se/verve/commit/7420be4ef0f4500dae2abaf033fc4ae10846e6d4))
* **backup:** answer a repeated manual backup request with the active job ([#191](https://github.com/gauthier-se/verve/issues/191)) ([6df1f45](https://github.com/gauthier-se/verve/commit/6df1f45129e8a726095d8fcf4161676c59373f04))
* **backup:** back up before migrations ([#186](https://github.com/gauthier-se/verve/issues/186)) ([a5ac23b](https://github.com/gauthier-se/verve/commit/a5ac23bb9a2bd7df4673a75c44d9d56ac8084ae3))
* **backup:** create backup archives and add verve backup ([#183](https://github.com/gauthier-se/verve/issues/183)) ([ba20a75](https://github.com/gauthier-se/verve/commit/ba20a754856d6631299ad94a7a628aa7ee6b4533))
* **backup:** manage backups through the admin API ([#189](https://github.com/gauthier-se/verve/issues/189)) ([dfd263f](https://github.com/gauthier-se/verve/commit/dfd263fced533e0443498e1b372d9d5d1cdad24c))
* **backup:** restore a backup with verve restore ([#188](https://github.com/gauthier-se/verve/issues/188)) ([7462c07](https://github.com/gauthier-se/verve/commit/7462c07af789890f1995ae6f8799564a62730c73))
* **backup:** schedule backups and apply retention ([#185](https://github.com/gauthier-se/verve/issues/185)) ([e82bd6f](https://github.com/gauthier-se/verve/commit/e82bd6f06c20db64f5a16f6f95eaa93f1332ec96))
* **config:** load or generate the secret key ([#146](https://github.com/gauthier-se/verve/issues/146)) ([d34d9a9](https://github.com/gauthier-se/verve/commit/d34d9a991e52db1c19667ef4b3e25fe44cd32a1a))
* **ingest:** accept native pushes through the inbox ([#238](https://github.com/gauthier-se/verve/issues/238)) ([caf4980](https://github.com/gauthier-se/verve/commit/caf4980048a23c39d4c8f5c1f38b9beeb3336557))
* **ingest:** decode the native format and apply its deletions ([#236](https://github.com/gauthier-se/verve/issues/236)) ([2a14f6d](https://github.com/gauthier-se/verve/commit/2a14f6d56011d470853521c87b39c03b6a4bb33a))
* **ingest:** detect the kind of a source ([#217](https://github.com/gauthier-se/verve/issues/217)) ([ed0a614](https://github.com/gauthier-se/verve/commit/ed0a61480faa6619c34336fa95ac402d87553614))
* **ingest:** detect the kind of sources known only by their name ([#223](https://github.com/gauthier-se/verve/issues/223)) ([d38909e](https://github.com/gauthier-se/verve/commit/d38909e93b7a866234acd0fe28aa760879a782d9))
* **ingest:** list and cancel imports, show user data to admins ([#240](https://github.com/gauthier-se/verve/issues/240)) ([0a4093b](https://github.com/gauthier-se/verve/commit/0a4093b1559421fd86d878c11623255d3440d02e))
* **ingest:** normalize records: units, times and validation ([#233](https://github.com/gauthier-se/verve/issues/233)) ([ec0c7a7](https://github.com/gauthier-se/verve/commit/ec0c7a73c87fe2db956ab31ce977f351614b8f4b))
* **ingest:** run records through the ingestion pipeline ([#235](https://github.com/gauthier-se/verve/issues/235)) ([1b7ba88](https://github.com/gauthier-se/verve/commit/1b7ba885e7089256edbe3b5b53ff8ae7b0a5b46f))
* **ingest:** ship the metric type catalog with the binary ([#213](https://github.com/gauthier-se/verve/issues/213)) ([1cdc160](https://github.com/gauthier-se/verve/commit/1cdc160ac7dec1b98940e7459dc19d6b1b5136b9))
* **ingest:** ship the workout activity types with the binary ([#219](https://github.com/gauthier-se/verve/issues/219)) ([b5b9951](https://github.com/gauthier-se/verve/commit/b5b995121ac5544fd170571a99285bbe0cf8020a))
* **jobs:** add the jobs table and repository ([#129](https://github.com/gauthier-se/verve/issues/129)) ([170c7bf](https://github.com/gauthier-se/verve/commit/170c7bf2d34a62ff53835e7fe43a2670f0809926))
* **jobs:** retry, cancel and cleanly stop jobs ([#137](https://github.com/gauthier-se/verve/issues/137)) ([77146e1](https://github.com/gauthier-se/verve/commit/77146e10574ac93be352dc11ef17b41358d5f7d2))
* **jobs:** run a job in-process under its serial key ([#149](https://github.com/gauthier-se/verve/issues/149)) ([ea08904](https://github.com/gauthier-se/verve/commit/ea08904a185ee90b8a3ef4aaa3604d6787789a71))
* **jobs:** run jobs with a worker pool, leases and heartbeats ([#136](https://github.com/gauthier-se/verve/issues/136)) ([8e2f633](https://github.com/gauthier-se/verve/commit/8e2f633abde3b6fa4956ac600918f98225163254))
* **jobs:** schedule recurring jobs ([#147](https://github.com/gauthier-se/verve/issues/147)) ([61bd92d](https://github.com/gauthier-se/verve/commit/61bd92d128cf23a40ae0676def9e75d163431627))
* **store:** compute hourly and daily aggregates after source selection ([#253](https://github.com/gauthier-se/verve/issues/253)) ([f58d448](https://github.com/gauthier-se/verve/commit/f58d448aef7117cf1abfaaab42e23848bba2509d))
* **store:** open the database next to a running server ([#132](https://github.com/gauthier-se/verve/issues/132)) ([591f573](https://github.com/gauthier-se/verve/commit/591f5738d4d430d02c38077fc1123811b868134f))
* **store:** register sources and resolve their aliases ([#215](https://github.com/gauthier-se/verve/issues/215)) ([621832e](https://github.com/gauthier-se/verve/commit/621832e65ffe440b041fe2c89f0c662cb9c34338))
* **store:** resolve source priorities with their defaults ([#252](https://github.com/gauthier-se/verve/issues/252)) ([01750b0](https://github.com/gauthier-se/verve/commit/01750b0dd45a91701e321a8bd19a32307a2a4e6a))
* **store:** store imports and their reports ([#234](https://github.com/gauthier-se/verve/issues/234)) ([8c1187f](https://github.com/gauthier-se/verve/commit/8c1187f8ca7ea38121b913696da710ae179aa275))
* **store:** store metric types and create uncurated types on first sight ([#214](https://github.com/gauthier-se/verve/issues/214)) ([ba241ce](https://github.com/gauthier-se/verve/commit/ba241ce5197161126708304b9ec97704fcbd3813))
* **store:** store samples and list the metric types with their data availability ([#218](https://github.com/gauthier-se/verve/issues/218)) ([21bb036](https://github.com/gauthier-se/verve/commit/21bb036c8eeff8214ae6eb9ac308f2889b67bd03))
* **store:** store workout routes and delete them with the account ([#222](https://github.com/gauthier-se/verve/issues/222)) ([d0c1372](https://github.com/gauthier-se/verve/commit/d0c1372557512aaf88554bcec1129581bab8eabf))
* **store:** store workouts and delete them with the account ([#220](https://github.com/gauthier-se/verve/issues/220)) ([35de22d](https://github.com/gauthier-se/verve/commit/35de22d46811d786dae6893b3295d97232d94082))
* **web:** build the authenticated shell and the login page ([#166](https://github.com/gauthier-se/verve/issues/166)) ([e955a77](https://github.com/gauthier-se/verve/commit/e955a77a82bab91680f6b39d101f2e138bffdf40))
* **web:** build the setup wizard ([#171](https://github.com/gauthier-se/verve/issues/171)) ([1e60c23](https://github.com/gauthier-se/verve/commit/1e60c23f4272b2d1f1624142578b54671fdcc5e1))
* **web:** build the tokens page ([#196](https://github.com/gauthier-se/verve/issues/196)) ([eb68807](https://github.com/gauthier-se/verve/commit/eb68807afbb90eca319272c194f00a235885f960))
* **web:** show the imports page and the running import banner ([#241](https://github.com/gauthier-se/verve/issues/241)) ([e17d668](https://github.com/gauthier-se/verve/commit/e17d6686b6d49fbac48cc8f60530681d1fbc5362))
* **workouts:** encode workout routes ([#221](https://github.com/gauthier-se/verve/issues/221)) ([f0af453](https://github.com/gauthier-se/verve/commit/f0af45375412e1a4b78f5d9c64a2c0426e691eab))


### Bug Fixes

* **auth:** answer 409 to a setup overtaken by another one ([#170](https://github.com/gauthier-se/verve/issues/170)) ([0c43e04](https://github.com/gauthier-se/verve/commit/0c43e0418b13e13507db581b025fead1a418f373))
* **ingest:** end the imports whose job a claim ends without a handler ([#242](https://github.com/gauthier-se/verve/issues/242)) ([62808a7](https://github.com/gauthier-se/verve/commit/62808a7f5d9caa168e0b42e6ce59af4ad6a65553))
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
