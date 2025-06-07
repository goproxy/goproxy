# Changelog

## [0.19.0](https://github.com/goproxy/goproxy/compare/v0.20.2...v0.19.0) (2025-06-07)


### ⚠ BREAKING CHANGES

* **Goproxy:** redesign `ErrorLogger` as `Logger` using `log/slog.Logger` ([#106](https://github.com/goproxy/goproxy/issues/106))

### Features

* add `Cache.MIMEType` ([1416280](https://github.com/goproxy/goproxy/commit/1416280f9da4c14a908f3ac42537ccee6f292c0d))
* add `cacher.MinIO.BucketLocation` ([15a5943](https://github.com/goproxy/goproxy/commit/15a59435468c8aae54485a49efcbcdc41c1e68ea))
* add `Cacher.NewHash` ([eefc8d7](https://github.com/goproxy/goproxy/commit/eefc8d7993782a94d987f601ab6020130c79fe0e))
* add `Fetcher` to fetch module files for `Goproxy` ([35f6d27](https://github.com/goproxy/goproxy/commit/35f6d27fc262a731ffdbed60b05ba4226b4eca5d)), closes [#46](https://github.com/goproxy/goproxy/issues/46)
* add `Goproxy.DisableNotFoundLog` ([689264d](https://github.com/goproxy/goproxy/commit/689264d3a01f6af973480e0ded0d76a12029835c))
* add `Goproxy.GoBinEnv` ([7efe0d1](https://github.com/goproxy/goproxy/commit/7efe0d178bc02f19ccaba2bc2af283bfb1d25803))
* add `Goproxy.GoBinFetchTimeout` ([b67483b](https://github.com/goproxy/goproxy/commit/b67483bd3534f5de05b8b69f1e2e7950086bbc56))
* add `Goproxy.InsecureMode` ([d6e5a3c](https://github.com/goproxy/goproxy/commit/d6e5a3c95c2cafc5da951bdf63c16d95350c25ad))
* add `Goproxy.MaxZIPCacheBytes` ([556dc35](https://github.com/goproxy/goproxy/commit/556dc35cb4f66c8cbe298335f2ec46f02b33cd8d))
* add `Goproxy.ProxiedSUMDBURLs` ([6475c98](https://github.com/goproxy/goproxy/commit/6475c98c57762d75c03371e9dd193b8365f4db78))
* add `Goproxy.TempDir` ([46e02ef](https://github.com/goproxy/goproxy/commit/46e02efdb5e4d24b11e3719d2a472c1767a4709a)), closes [#27](https://github.com/goproxy/goproxy/issues/27)
* add `httpDo` ([ee91433](https://github.com/goproxy/goproxy/commit/ee914331fd0f96ca265f687326baa9205a0792b1))
* add `modClean` ([f3f6a02](https://github.com/goproxy/goproxy/commit/f3f6a0231ac0f8a757ebb080a585d4aa8e53f6de))
* add Alibaba Cloud Object Storage Service support ([4cb1818](https://github.com/goproxy/goproxy/commit/4cb1818dacde0ea6bc47758a5a47e20c82d0cfb0))
* add Amazon Simple Storage Service support ([42bd5ce](https://github.com/goproxy/goproxy/commit/42bd5ce57114b8a1bf1f613cef259d05d33ba297))
* add built-in GONOPROXY support ([7102a9d](https://github.com/goproxy/goproxy/commit/7102a9da8d138a374f45508a395c64facda6c8ab))
* add built-in GOPRIVATE support ([b9c9b16](https://github.com/goproxy/goproxy/commit/b9c9b1694c4f4bb3b1c68784dedf01d218b87b60))
* add built-in GOSUMDB and GONOSUMDB support ([d97a712](https://github.com/goproxy/goproxy/commit/d97a712ebedc2779486cbd7dc556755ef8727cf2))
* add CLI implementation ([cdc4fb1](https://github.com/goproxy/goproxy/commit/cdc4fb1e9d06915554ee2ce2d4e48f8c8e9e8260)), closes [#33](https://github.com/goproxy/goproxy/issues/33)
* add DigitalOcean Spaces support ([397f9ff](https://github.com/goproxy/goproxy/commit/397f9ffa830ce2d4eede4eca3af13d0b64a45298))
* add Disable-Module-Fetch header support ([5154c7b](https://github.com/goproxy/goproxy/commit/5154c7b060b92124f206355f9d13e183f1660deb)), closes [#39](https://github.com/goproxy/goproxy/issues/39)
* add Google Cloud Storage support ([edfd58b](https://github.com/goproxy/goproxy/commit/edfd58bf6351e8ee8ba5bcf8ee02048555baa6eb))
* add Microsoft Azure Blob Storage support ([ea53e5b](https://github.com/goproxy/goproxy/commit/ea53e5b4145e6fe09ff53c7e4307d87011f20a0f))
* add MinIO support ([b68bf1c](https://github.com/goproxy/goproxy/commit/b68bf1ced14646da545485b38994175fa905a2b9))
* add pipe (|) support to built-in GOPROXY support ([#14](https://github.com/goproxy/goproxy/issues/14)) ([062fc67](https://github.com/goproxy/goproxy/commit/062fc67148aeb0a121fda67d879beb5d3af4325f))
* add Qiniu Cloud Kodo support ([c3aa0ea](https://github.com/goproxy/goproxy/commit/c3aa0ea9f3677cb88833e287aeeaafadbbf2ec50))
* add retry support for URL determination in `sumdbClientOps` ([bcbfe5c](https://github.com/goproxy/goproxy/commit/bcbfe5cb7ce0a69f797c4d24afe0f55d4ff97ad5))
* add virtual hosted endpoint support ([5181dfa](https://github.com/goproxy/goproxy/commit/5181dfae612be37680e8c55bddd5dbbbaaf9f5e5))
* cache module files for all valid operations ([5da185b](https://github.com/goproxy/goproxy/commit/5da185bd2f83c4179cb54b542476f0d90322a17b))
* cache module version list ([e3edbe4](https://github.com/goproxy/goproxy/commit/e3edbe46a707abcec4c7c73acc84713741c74c25))
* cache proxied checksum database content ([040c85e](https://github.com/goproxy/goproxy/commit/040c85eb77a01506d86ede101a116eb4326545b6))
* **Cacher:** support optional `interface{ Size() int64 }` for Content-Length header ([#60](https://github.com/goproxy/goproxy/issues/60)) ([546d218](https://github.com/goproxy/goproxy/commit/546d21817ed7ccf9fd925ee3262ce23dfa4aeb5c))
* **cmd/goproxy:** add `--log-format` to `server` command ([#109](https://github.com/goproxy/goproxy/issues/109)) ([efa2ced](https://github.com/goproxy/goproxy/commit/efa2ced1005faab49a4828e86c31c724e8454a30))
* **cmd/goproxy:** add file URI scheme support ([8bc16cc](https://github.com/goproxy/goproxy/commit/8bc16cc0d60cf4c1f3d8d4d2d104e2aa26b34161))
* **cmd/goproxy:** add graceful shutdown support for `server` ([1577139](https://github.com/goproxy/goproxy/commit/1577139a82b581b9622ed2e6e221dacbdf2a25e8))
* **cmd/goproxy:** add S3 cacher support for `server` ([06671ed](https://github.com/goproxy/goproxy/commit/06671ed85120fc5567e0aa6b6f3905ce53abcc29))
* implement auto-retry mechanism for `httpGet` ([7fe9350](https://github.com/goproxy/goproxy/commit/7fe9350870611b186e5df06f360141f5b9f137c6))
* use exponential backoff algorithm in `httpGet` ([2a31997](https://github.com/goproxy/goproxy/commit/2a319979eb82dfe5b30966e3ace34b1bcf134bde))


### Bug Fixes

* avoid removing persistent files ([9370087](https://github.com/goproxy/goproxy/commit/93700871d74c204d406e34773ecaca1e2a769edf))
* **cmd/goproxy:** require github.com/minio/crc64nvme@v1.0.1 ([#100](https://github.com/goproxy/goproxy/issues/100)) ([6e61c8f](https://github.com/goproxy/goproxy/commit/6e61c8fb617be2e69af7164e446364c3b06928da))
* correct `cacher.MinIO.Cache` ([f5ac410](https://github.com/goproxy/goproxy/commit/f5ac41031fbc00fa9859c3aed48f2a6e4514bf08))
* correct `cachers.gcsCache.Read` and `cachers.kodoCache.Read` ([461dbca](https://github.com/goproxy/goproxy/commit/461dbca069dca2a4d2ae24a5879aae3ba8fa920f))
* correct `cachers.MinIO.load` ([8db30a4](https://github.com/goproxy/goproxy/commit/8db30a4e47463a2dd5b28099fcd4822fc416d1d2))
* correct `cachers.OSS.Cache` ([16bf28a](https://github.com/goproxy/goproxy/commit/16bf28add86dc4d23aec91e2179f226a182b502e))
* correct `cachers.OSS.load` ([d65e44c](https://github.com/goproxy/goproxy/commit/d65e44cf5aad404028250187a11e62bb30c8a2a0))
* correct `mod` ([4040498](https://github.com/goproxy/goproxy/commit/40404984165d12b594b127665f06653efeca630f))
* correct `mod` ([670c16f](https://github.com/goproxy/goproxy/commit/670c16f2a24672283eddc30d04e7ff6592a0e197))
* correct `regModuleVersionNotFound` ([9218d66](https://github.com/goproxy/goproxy/commit/9218d669c3b72ebea0f7e216f9508c1120485932))
* correct auto-retry on `http.Client.Do` error for `httpGet` ([39d1074](https://github.com/goproxy/goproxy/commit/39d1074b493a73007382f95e2e6813e7a2f21705))
* correct behavior of `tempCache.Close` ([b531d8a](https://github.com/goproxy/goproxy/commit/b531d8a7e9331b9b5969a6f4f3bf189c32ad0b2b))
* correct built-in GOPROXY=off behavior ([e7b1931](https://github.com/goproxy/goproxy/commit/e7b193158945da3787c4be1eb23caf483dc5ac01))
* correct built-in GOSUMDB implementation ([67f6616](https://github.com/goproxy/goproxy/commit/67f66166621ab848467ad7608ee0317e332d4688))
* correct built-in GOSUMDB support ([7f3667c](https://github.com/goproxy/goproxy/commit/7f3667c91fa60f2d6ee6bf8e03ea5b932c7699b5))
* correct cache checksum computing of Qiniu Cloud Kodo ([cfa5347](https://github.com/goproxy/goproxy/commit/cfa5347fd501c7df9d66dcd887a3389dc9d9c2af))
* correct Cache-Control header management ([bd212f1](https://github.com/goproxy/goproxy/commit/bd212f102dc3a0a3011cf4ced613a056f69cbef1))
* correct error handling ([37a8141](https://github.com/goproxy/goproxy/commit/37a81413eee29dca54a0eb11680e17bdb240b7b0))
* correct error handling in `sumdbClientOps.load` ([34d5dcf](https://github.com/goproxy/goproxy/commit/34d5dcfbb37745824d2bd0d55beb8426f5d5f5b4))
* correct GOPRIVATE behavior ([635a05a](https://github.com/goproxy/goproxy/commit/635a05a39f3338075945b6a1c05273d97aa6fa21))
* correct module path and version matching in `Goproxy.serveFetch` ([d5c240a](https://github.com/goproxy/goproxy/commit/d5c240a7cbce0478adb384d7f783d009193b7878))
* correct proxying checksum database support ([8edbf95](https://github.com/goproxy/goproxy/commit/8edbf959bc61502687dae2d7d28209d09a270934))
* correct proxying checksum database support ([510c7b2](https://github.com/goproxy/goproxy/commit/510c7b2b78f3888ff8b3e95be24cc5606244377d))
* correct some improper stuff ([#13](https://github.com/goproxy/goproxy/issues/13)) ([fecaf5c](https://github.com/goproxy/goproxy/commit/fecaf5c0ed0a5c747a56d25bdf316469d1015f67))
* correct temporary file reading ([4aab780](https://github.com/goproxy/goproxy/commit/4aab78027136daaa2503d13400f12c72e54db57e))
* correct timeout control ([dffe6d0](https://github.com/goproxy/goproxy/commit/dffe6d0198ecc71d2bb159f850469f19649969d8))
* **Dockerfile:** extract binaries from tarball when using GoReleaser artifacts ([#87](https://github.com/goproxy/goproxy/issues/87)) ([2b9d89c](https://github.com/goproxy/goproxy/commit/2b9d89c41e3724b4718637935693d59e6c94df34))
* **Dockerfile:** git lfs install global ([#56](https://github.com/goproxy/goproxy/issues/56)) ([4254665](https://github.com/goproxy/goproxy/commit/4254665ab65c1d6caee6a1ea4cabf9039bc60099))
* **GoFetcher:** include `latest` query when performing `directList` ([#73](https://github.com/goproxy/goproxy/issues/73)) ([9dad311](https://github.com/goproxy/goproxy/commit/9dad311a82c3984a083ff0598cbed212ea7db38e))
* remove shadowed var `content` in else block to avoid passing a nil to io.Copy() ([#23](https://github.com/goproxy/goproxy/issues/23)) ([862e2ae](https://github.com/goproxy/goproxy/commit/862e2ae0e015fa784a03609f7139d776c0a66f0c))
* set status code in responseString ([5279985](https://github.com/goproxy/goproxy/commit/52799853012f1468d1fc2ce19575d40076aa7606))
* set status code in responseString ([523a2a5](https://github.com/goproxy/goproxy/commit/523a2a534ddc8baa8ac07edc06447beb3725f8f4))


### Code Refactoring

* add `httpGetTemp` to simplify code ([2b4a7a0](https://github.com/goproxy/goproxy/commit/2b4a7a04ef79072fd47a8588cd86f0bc04e8c5ef))
* add `responseStatusCode` to simplify code ([01da90b](https://github.com/goproxy/goproxy/commit/01da90b156a3ebb3cdf7b4307012a61952b0785d))
* add checks for built-in GOPROXY support ([cc2e19e](https://github.com/goproxy/goproxy/commit/cc2e19e77b6b0c7e085073c3620cbd72541009a3))
* add default value for `cachers.OSS.Endpoint` ([0594aaf](https://github.com/goproxy/goproxy/commit/0594aaf50df666dc256f2e16ae6a4d4b3828ffef))
* add mapstructure tag for `Goproxy.InsecureMode` ([4ff0832](https://github.com/goproxy/goproxy/commit/4ff08324a4128dd6153c9b03bf7b800a07e2d1f8))
* add more info to error logging ([a636e8e](https://github.com/goproxy/goproxy/commit/a636e8ed709caf9360e67f8f621d7a0b66dec9ae)), closes [#31](https://github.com/goproxy/goproxy/issues/31)
* add more retryable status codes to `httpGet` ([d17cce3](https://github.com/goproxy/goproxy/commit/d17cce318f90f0f179f46c1ca77bab4e798762c8))
* adjust timeout limit for cache uploads ([1b0db40](https://github.com/goproxy/goproxy/commit/1b0db40df49a142a85a47c8d5a75f8ee2417baa9))
* allow zero time in `unmarshalInfo` ([44dde84](https://github.com/goproxy/goproxy/commit/44dde8436b101be9b2ee311d6e4a9919392a2a68))
* allow zero-time info file ([ea4847a](https://github.com/goproxy/goproxy/commit/ea4847a3b06aea179debf0f93b98e3c555b4253a))
* avoid adding duplicate prefix in `responseNotFound` ([c694229](https://github.com/goproxy/goproxy/commit/c694229ac5d4fb4400ca01e06865943319572800))
* avoid executing go command when setting GOPROXY ([79b37a9](https://github.com/goproxy/goproxy/commit/79b37a9ee7fb9ebb83b600432d8ed73894697c66))
* avoid temp caches being accidentally closed ([3159cd8](https://github.com/goproxy/goproxy/commit/3159cd89dd054177c22e1a4a3c4de4763a341499))
* bring back checks for mod and zip files ([2ec8f36](https://github.com/goproxy/goproxy/commit/2ec8f36dbbfbdfac12fc031bf103cd5d45142d0a))
* bump golang.org/x/mod to v0.13.0 ([d832cfd](https://github.com/goproxy/goproxy/commit/d832cfd08e2df496a1b9053514e290abd1ba5a2a))
* bump minimum required Go version to 1.22.0 ([#79](https://github.com/goproxy/goproxy/issues/79)) ([7e4176b](https://github.com/goproxy/goproxy/commit/7e4176be1f233a2e069f6313e6ce5407bf2ec05a))
* bump minimum required Go version to 1.23.0 ([#112](https://github.com/goproxy/goproxy/issues/112)) ([f1c66d7](https://github.com/goproxy/goproxy/commit/f1c66d79c98bc2bf01eb44d66ab01e4e343bf314))
* bump minimum supported Go version to 1.18 ([586904d](https://github.com/goproxy/goproxy/commit/586904d1f33127c55b93259079b895a3df0f857c))
* change default value of `Goproxy.MaxGoBinWorkers` ([53a9792](https://github.com/goproxy/goproxy/commit/53a97920cc5cf97b0740695e07c85bf8a5f5cdcd))
* change permissions for auto-created DirCacher to 0750 ([#25](https://github.com/goproxy/goproxy/issues/25)) ([233bbca](https://github.com/goproxy/goproxy/commit/233bbca671b69f8d237afd79e7faa9ec40c646f6))
* change permissions for directories created by `DirCacher.Put` to 0755 ([fd12ce2](https://github.com/goproxy/goproxy/commit/fd12ce23c33dcb35aff81cfbc8381de7e5b6f38a))
* check for canonical module versions in `GoFetcher.Download` ([a1e2a77](https://github.com/goproxy/goproxy/commit/a1e2a7768193c47757fa272ae351195ebae03c7d))
* clean up misuse of `fmt.Sprint` and `fmt.Sprintf` ([8c82e46](https://github.com/goproxy/goproxy/commit/8c82e4625e1577ac75bfcdf03a25f27c783415b6))
* **cmd/goproxy:** add -connect-timeout ([fe8d8d0](https://github.com/goproxy/goproxy/commit/fe8d8d06e0d7114ca0e8affd19194e912c0c0a01))
* **cmd/goproxy:** add -fetch-timeout ([ac2d6e4](https://github.com/goproxy/goproxy/commit/ac2d6e4849f84d2854132d92bcc8e2500e34654c))
* **cmd/goproxy:** default -connect-timeout to 30s ([0704044](https://github.com/goproxy/goproxy/commit/07040442e09f0be7dd12c4b7b233e320b71eee46))
* **cmd/goproxy:** default -go-bin-max-workers to 0 ([3dba9bd](https://github.com/goproxy/goproxy/commit/3dba9bd306531564ac0a6afb5f55c2a38e170c7b))
* **cmd/goproxy:** default `--fetch-timeout` to 10m ([466a949](https://github.com/goproxy/goproxy/commit/466a949a8a94afca857fd8b23020cc8c07e70c65))
* **cmd/goproxy:** ignore empty ETag values for S3 caches ([63f292a](https://github.com/goproxy/goproxy/commit/63f292a71e9ac9749bf212ab849cf2aa0563bd95))
* **cmd/goproxy:** improve flag usages ([e523687](https://github.com/goproxy/goproxy/commit/e523687637acc202f1a2da5d346c3964218e367b))
* **cmd/goproxy:** inject version info at build time ([639e995](https://github.com/goproxy/goproxy/commit/639e99533f5ffa3430d43897177bdc15a36bad0d))
* **cmd/goproxy:** move `internal.binaryVersion` to `internal.go` file ([e1d0b32](https://github.com/goproxy/goproxy/commit/e1d0b32a562b16ce3fcf84c4ec3fac067d725e74))
* **cmd/goproxy:** rename `--cacher-dir` to `--cache-dir` ([5554a02](https://github.com/goproxy/goproxy/commit/5554a027b4eebefe667d3820469a7bc5e270bd14))
* **cmd/goproxy:** use github.com/spf13/cobra to redesign all ([b697094](https://github.com/goproxy/goproxy/commit/b6970940b72914d07d5001761cfffaff015fd8a3))
* control timeout ([c467a97](https://github.com/goproxy/goproxy/commit/c467a976059ac3fdf5446242edf512ab1eb2cf10))
* correct blocking behavior of `mod` ([6101e96](https://github.com/goproxy/goproxy/commit/6101e966e2701f9e0848d4c5d4a26c96f6f30a7e))
* correct mod error message ([24b697a](https://github.com/goproxy/goproxy/commit/24b697a480513fd7cf9f75859db68efa02c0db64))
* disable multipart upload for `cacher.MinIO` ([2733487](https://github.com/goproxy/goproxy/commit/2733487cae32b6b0dae8b576888fb4e789e4f4be))
* disallow resolving version "latest" ([9e88b83](https://github.com/goproxy/goproxy/commit/9e88b8352987bd2527711c9fc7dbf0ec365e235d))
* do not check scheme in `parseRawURL` ([da5febd](https://github.com/goproxy/goproxy/commit/da5febd3ede45aef6c35e57a8f32877d651e7523))
* do not force local caches created by `fetch.doDirect` to be writable ([e7ba05e](https://github.com/goproxy/goproxy/commit/e7ba05e1aecdf29c5183308b02a95e9b81dfe3a8))
* enable multipart upload for `cacher.MinIO` ([1a9242a](https://github.com/goproxy/goproxy/commit/1a9242a5cbe364a53319ebcc50ce9e78a76dab55))
* format info content ([bd0b9b7](https://github.com/goproxy/goproxy/commit/bd0b9b77d097f5b53d2e4476a53f7aeed0888c35))
* format temporary file name created by `Cacher.Set` ([af9c030](https://github.com/goproxy/goproxy/commit/af9c03007f91cd2191aed304ded6f5624af63300))
* get rid of 80-column rule to make code more compact ([d0241fc](https://github.com/goproxy/goproxy/commit/d0241fc4e17f87fd134b40eb6f2cd9a583d737bc))
* **Goproxy:** redesign `ErrorLogger` as `Logger` using `log/slog.Logger` ([#106](https://github.com/goproxy/goproxy/issues/106)) ([ab925cf](https://github.com/goproxy/goproxy/commit/ab925cf087583688ac8745206355a5c53d6388cc))
* ignore `context.Canceled` when it's harmless ([6dae7dc](https://github.com/goproxy/goproxy/commit/6dae7dcb039aae7db6b50437c1f533610fd98a9a))
* ignore logs in `sumdbClientOps.SecurityError` ([bdd9613](https://github.com/goproxy/goproxy/commit/bdd96137f27a9d5bcf596550246f2345030eaada))
* improve `appendURL` ([afab12b](https://github.com/goproxy/goproxy/commit/afab12b8ac509b94e0a5015de36db25c3521d455))
* improve `cacher.MinIO.Cache` ([52faea2](https://github.com/goproxy/goproxy/commit/52faea209aa6c9890b5a1980b32d404761930319))
* improve `checkModFile` and `checkZipFile` ([1dbfcc4](https://github.com/goproxy/goproxy/commit/1dbfcc4c0750cd293a38c7fa41f5d6f3ff292709))
* improve `DirCacher` ([ea184c9](https://github.com/goproxy/goproxy/commit/ea184c9f9fa2c8f9590229988c279a6ad7c3de04))
* improve `Goproxy.GoBinEnv` parsing ([bd00756](https://github.com/goproxy/goproxy/commit/bd00756a5d899c6a8481453b9e6d02902482fd7f))
* improve `Goproxy.httpClient` ([5cb50b6](https://github.com/goproxy/goproxy/commit/5cb50b653c7e4b3f37cee1615c96c071744e80ce))
* improve `httpGet` ([b82d87d](https://github.com/goproxy/goproxy/commit/b82d87d33ebf4b72802289042e232543063b95ca))
* improve `isTimeoutError` ([c30189b](https://github.com/goproxy/goproxy/commit/c30189b1564b88807d9608985e74affba626a2f7))
* improve `mod` ([accc231](https://github.com/goproxy/goproxy/commit/accc2313f6158e89c278bcbea2859739e6dd0ad5))
* improve `responseModError` ([a7efe76](https://github.com/goproxy/goproxy/commit/a7efe76b5c5fd5b6b47c10d058f44364b86c713a))
* improve `setResponseCacheControlHeader` ([ad50b8c](https://github.com/goproxy/goproxy/commit/ad50b8c81316a25fbb71af99f3e856c9862884d1))
* improve cache timeout control ([921935e](https://github.com/goproxy/goproxy/commit/921935e9a7e68ef7bad0eb90de00e7c5d4d4771e))
* improve Cache-Control header management ([8fa4396](https://github.com/goproxy/goproxy/commit/8fa43965021244990e530cc8ffd2c5b0350ae048))
* improve Cache-Control header management ([86f764a](https://github.com/goproxy/goproxy/commit/86f764a823156f8d69e0ab436113c5ce57a71ad3))
* improve Cache-Control header management ([d054d51](https://github.com/goproxy/goproxy/commit/d054d5169ca10e632f3ac1b233e7a6adf7c78081))
* improve Cache-Control header management ([d38aa69](https://github.com/goproxy/goproxy/commit/d38aa69561939fc90e330de203e7656d7cab6aaa))
* improve Cache-Control header management ([4564395](https://github.com/goproxy/goproxy/commit/45643959f59eeefec8b675138b28fa46d45f3dfa))
* improve Cache-Control header management ([fcb2e0e](https://github.com/goproxy/goproxy/commit/fcb2e0e99da7e8c412ebd1bce9d67d35ff23bb46))
* improve Cache-Control header management ([b4856f4](https://github.com/goproxy/goproxy/commit/b4856f4a827836815c8968b31f50cfa443b12ed8))
* improve Cache-Control header management ([5cabe8a](https://github.com/goproxy/goproxy/commit/5cabe8ac4ee0e8f70460f60dcfd7222cd5da98b4))
* improve Cache-Control header management ([eaf01e3](https://github.com/goproxy/goproxy/commit/eaf01e3c7ddb15ae00bf1cc32863cbd531a4049b))
* improve Cache-Control header management for invalid requests ([f7a8991](https://github.com/goproxy/goproxy/commit/f7a89917aebb09ac12a496f6e88565829a49e031))
* improve checksum retrieval for `cacher.MinIO` ([c895471](https://github.com/goproxy/goproxy/commit/c89547155086965f9a4d790f4777c3f2a97a9965))
* improve checksum retrieval for `cacher.MinIO` ([f72bf39](https://github.com/goproxy/goproxy/commit/f72bf392137296125549560e4eb7a1b1f2b76fec))
* improve error handling ([ab7d067](https://github.com/goproxy/goproxy/commit/ab7d067f84489e54b506f1a0a0e4298b32da4c5a))
* improve error handling ([b2c79b3](https://github.com/goproxy/goproxy/commit/b2c79b3f1a60d1d2c6f5fe619b701e979e56634c))
* improve error handling ([5d6d641](https://github.com/goproxy/goproxy/commit/5d6d641bb82dab2fe377f7fbf0399bbe9e4f7ccb))
* improve error handling in `GoFetcher.execGo` ([5d37c0d](https://github.com/goproxy/goproxy/commit/5d37c0d363a2b4e976b29619d5b47fcf5ad8d3da))
* improve error logging ([418b4ac](https://github.com/goproxy/goproxy/commit/418b4ac8acbee18a95e62992e6c2546bc83cba16))
* improve error logging ([80534b0](https://github.com/goproxy/goproxy/commit/80534b0bf9a161f6aa9bf6dfa8a6ed351ed1dc45))
* improve error message formatting ([#95](https://github.com/goproxy/goproxy/issues/95)) ([faf43bd](https://github.com/goproxy/goproxy/commit/faf43bd21170ae02274378cf75193c3cd8da0541))
* improve error message of `mod` ([cb9d791](https://github.com/goproxy/goproxy/commit/cb9d7916581ebe2bb486c72e5fbfbbf7c8d60b71))
* improve error messages ([e473cf8](https://github.com/goproxy/goproxy/commit/e473cf89d8413235b138749baea4b6aacbb12bcd))
* improve error messages ([a570108](https://github.com/goproxy/goproxy/commit/a570108bf01f0d218530b614884841fe1a1a7a7e))
* improve error messages ([957b730](https://github.com/goproxy/goproxy/commit/957b730531943ffdb62810804335d754e843649b))
* improve error messages ([2729517](https://github.com/goproxy/goproxy/commit/2729517799220884b10d992ab1246e5f96940122))
* improve go error parsing in `fetch.doDirect` ([fd66e9d](https://github.com/goproxy/goproxy/commit/fd66e9daeb0ef602190c414e4acc50d641e66a98))
* improve GOPROXY walking ([fe86455](https://github.com/goproxy/goproxy/commit/fe864554ed7c328aaa60f919eda5b8a40fe7baf7))
* improve handling of environment variables related to Go modules ([28f90c8](https://github.com/goproxy/goproxy/commit/28f90c8a0e945f022e9b6b3d37d88128905a32a5))
* improve invalid version error message for `unmarshalInfo` ([1b1dd38](https://github.com/goproxy/goproxy/commit/1b1dd38e2d0b1cdc78419e3d0001157a7ec52579))
* improve module download and verification ([734b544](https://github.com/goproxy/goproxy/commit/734b5447be2537265c374882c53ca78becbd142a))
* improve module file caching ([a12c598](https://github.com/goproxy/goproxy/commit/a12c598eadda021134068c2c70cc0c8889d07e26))
* improve module file checking and formatting ([51bb6cd](https://github.com/goproxy/goproxy/commit/51bb6cd72a607cf251cc580126dce7a0888f7e13))
* improve module files checking ([8c1232c](https://github.com/goproxy/goproxy/commit/8c1232c16f63fc684bbbea5d80c335d0a7042dcd))
* improve module version not found handling ([7ee3cfe](https://github.com/goproxy/goproxy/commit/7ee3cfe438d48899541fd9e689fdc8acd2d24539))
* improve proxy list walking in `mod` ([e4635ae](https://github.com/goproxy/goproxy/commit/e4635ae1c6f1f99879de5c21c4825c4b7b8441f7))
* improve proxying checksum database support ([9d8367f](https://github.com/goproxy/goproxy/commit/9d8367f59ef7f5613cc30ed59534e970ddf3292f))
* improve proxying checksum database support ([95d3c18](https://github.com/goproxy/goproxy/commit/95d3c18919895c731415400762f53693cbd8d4f0))
* improve resource cleanup for `GoFetcher.Download` ([5708e28](https://github.com/goproxy/goproxy/commit/5708e28be9efcb8d4fadf8398a661dd5864bce34))
* improve response funcs ([29d5f5c](https://github.com/goproxy/goproxy/commit/29d5f5c00bb0cb7d351c011f68aedb6bee0b670d))
* improve retryable `http.Client.Do` error determination ([d262dfb](https://github.com/goproxy/goproxy/commit/d262dfb7f3683647aa60ea0b985ec7c3abaa4f5b))
* improve schemeless URL parsing ([725834d](https://github.com/goproxy/goproxy/commit/725834dea1060922eb9cf3087abc5b7b2675bbc6))
* improve temporary data cleaning ([1f802d8](https://github.com/goproxy/goproxy/commit/1f802d8f8a7cce9c78464de7997be0c34603df78))
* leave mod and zip files check to checksum database ([fccdd70](https://github.com/goproxy/goproxy/commit/fccdd702da598ec3bf19a338f35487d71257ea9b))
* lookup module version only for .info requests ([74d94e6](https://github.com/goproxy/goproxy/commit/74d94e6e922e1dc6e22fafac6d43e2525ee27db6))
* make `checkModFile` more easygoing ([69f3b13](https://github.com/goproxy/goproxy/commit/69f3b1391164395808350b2b9f5d32825e9f2837))
* make `checkModFile` more easygoing ([eb07f2f](https://github.com/goproxy/goproxy/commit/eb07f2fab9d1ec8940a9f9a7bfb2e4b802b351ce))
* make `DirCacher` create cache files with 0644 permissions ([eca3399](https://github.com/goproxy/goproxy/commit/eca3399ac3ff5663eaa4a6be48306095cbc4f924))
* make `formatInfoFile` more harmless ([d69f601](https://github.com/goproxy/goproxy/commit/d69f6013182e39efb0bcfb097c9a27fc5d70abe4))
* make `Goproxy.logError` always have a prefix ([0c05725](https://github.com/goproxy/goproxy/commit/0c057257846ed7a6e3de3aeb16ae4ce60d2a912e))
* make `notFoundError` matches `fs.ErrNotExist` ([3438e36](https://github.com/goproxy/goproxy/commit/3438e366b115d178ae2880aa9095e2c1ebf6ff4d))
* make `walkEnvGOPROXY` parse proxies before calling its `onProxy` ([0aae1f2](https://github.com/goproxy/goproxy/commit/0aae1f229b52bb52111aa1f47003b66eb78193a0))
* make built-in GOSUMDB adapt to built-in GOPROXY ([080f990](https://github.com/goproxy/goproxy/commit/080f99010796b6baea1ef05838ce3d1f22565287))
* make local caches created by `fetch.doDirect` writable ([5e9fff6](https://github.com/goproxy/goproxy/commit/5e9fff608baa88dd916b0d8b66170c98624dd595))
* make newly downloaded module files respond more quickly ([597b289](https://github.com/goproxy/goproxy/commit/597b289ac3d0a7499388bf5b5015ff8ce7fb1625))
* make nil `Goproxy.Cacher` safe to use ([ef9fc1a](https://github.com/goproxy/goproxy/commit/ef9fc1ae9d0c445169581ff3d39bb17a9eedc17c))
* make sure `fetch.doProxy` downloads all module files at once ([8e845a1](https://github.com/goproxy/goproxy/commit/8e845a18c85d12dd60c4337e4c0a63640aa00feb))
* make temporary directories more tidy ([38a8a77](https://github.com/goproxy/goproxy/commit/38a8a773a6ffb0035ab46e467c0cec7e24e9f888))
* make timeout fetches uncacheable ([c6b028c](https://github.com/goproxy/goproxy/commit/c6b028c8a0a419c05bd9dc6fe6117bc8bcd8a553))
* make up for `os.RemoveAll` failures ([352b5ce](https://github.com/goproxy/goproxy/commit/352b5ce0cc2f99f17f9e79774e94209be13aa77b))
* merge `Goproxy.ProxiedSUMDBNames` and `Goproxy.ProxiedSUMDBURLs` ([f3bb12b](https://github.com/goproxy/goproxy/commit/f3bb12b279d4ee8741fbb8f13a9b41172be10e80))
* move `backoffSleep` to http.go ([e1589b6](https://github.com/goproxy/goproxy/commit/e1589b61160838d6978291aaaffddabbd96e1516))
* move `stringSliceContains` to fetch.go ([2c357e0](https://github.com/goproxy/goproxy/commit/2c357e0850474a2fbf87e865cd417ebe5b138106))
* name temporary files/directories like mktemp utility ([d6c7083](https://github.com/goproxy/goproxy/commit/d6c70830a890a15873c6a87695bfc01b05033138))
* only accept requests with canonical paths that do not end with "/" ([02274ec](https://github.com/goproxy/goproxy/commit/02274ecbdc4ed7c8cdd086c900112673b25797c1))
* optimize cache ttl for pseudo-dynamic responses ([86e5cd7](https://github.com/goproxy/goproxy/commit/86e5cd7e506787b7a7ba3f30af268c7d740414e9))
* override GOTMPDIR in `executeGoCommand` ([8725ce5](https://github.com/goproxy/goproxy/commit/8725ce5c84237a2a5a4010425c61a57d8f8a7509))
* parse proxied checksum database URLs in `Goproxy.load` ([3052582](https://github.com/goproxy/goproxy/commit/3052582d0c1b96340552dabea8cebcfc6de95494))
* prevent trailing slash and .. in request path ([b5c324f](https://github.com/goproxy/goproxy/commit/b5c324f90b4c55567fdf3b38e5d43b6edbb5319d))
* print help messages for particular 404 errors ([05992e7](https://github.com/goproxy/goproxy/commit/05992e7ff8bb3419bb7bc8aa7b60b528bb118d0f))
* redesign `Cacher.Set` ([cc403db](https://github.com/goproxy/goproxy/commit/cc403db6c91b95e9c8d2566689648f7956a9251d))
* redesign `Cacher` and `Cache` ([531994d](https://github.com/goproxy/goproxy/commit/531994dc3a949c8616c7de33a61fa1733e94e4c9))
* redesign `Goproxy.logError` to `Goproxy.logErrorf` ([6ea3567](https://github.com/goproxy/goproxy/commit/6ea3567587d2602ae551488876db26053f57606e))
* redesign `notFoundError` to better wrap other errors ([4c97c75](https://github.com/goproxy/goproxy/commit/4c97c7541a50081e36e607df7ebcaa791a14ba7f))
* redesign all ([f288d4a](https://github.com/goproxy/goproxy/commit/f288d4ac6f5b178bb086c7bf11756b30f3d8ab37))
* redesign module fetch ([23dc691](https://github.com/goproxy/goproxy/commit/23dc6914e1aa06e682f777a440b4b29cbe3a575c))
* redesign optional interfaces for return value of `Cacher.Get` ([3ee27c4](https://github.com/goproxy/goproxy/commit/3ee27c445bf10eb5189831607e0242f11aa65feb))
* refine `cacher.Disk` ([9fcd7e1](https://github.com/goproxy/goproxy/commit/9fcd7e115839e28fc566554e7ffaa6bb2bc52bc0))
* refine `cachers.Disk` ([bfee3c4](https://github.com/goproxy/goproxy/commit/bfee3c4281a94314df7a93c817a3f9f757f511db))
* refine `Goproxy.ServeHTTP` ([f21b6eb](https://github.com/goproxy/goproxy/commit/f21b6eb4d86fc2b2d040caf89f262d6251d28180))
* refine built-in GOPROXY and GOSUMDB support ([bed6907](https://github.com/goproxy/goproxy/commit/bed6907c5a193d84f648b586227a9a230abcbbb5))
* refine code ([29f7880](https://github.com/goproxy/goproxy/commit/29f7880f2ba65e5aae16fd04693bbb4752af4054))
* refine comments ([0d2d9d4](https://github.com/goproxy/goproxy/commit/0d2d9d42607c935394528b8b2b2ec3062e38c41e))
* refine error handling ([8acd660](https://github.com/goproxy/goproxy/commit/8acd6603f28f5c2ec88192c6eb627951189a80de))
* refine error logging ([ddd6201](https://github.com/goproxy/goproxy/commit/ddd6201cc621a72e201a67c80c856d4783a07a9e))
* refine proxying checksum database support ([42ac7b6](https://github.com/goproxy/goproxy/commit/42ac7b6d4cabf7612b91be7f3c4bb21981c42e1d))
* reject requests ending with `/upgrade.info` or `/patch.info` ([abd0ce5](https://github.com/goproxy/goproxy/commit/abd0ce5100e8acedc2dfe4d2467557594922af8e))
* remove `fetch.contentType` ([527e7e9](https://github.com/goproxy/goproxy/commit/527e7e9718c089a812b1fc5dfb3d0c4a0df0ea0d))
* remove `fetchResult.Open` ([1dfc7df](https://github.com/goproxy/goproxy/commit/1dfc7dfd4929d4e783e0c7b2e1aee343b762f887))
* remove `Goproxy.CacherMaxCacheBytes` ([9dafb31](https://github.com/goproxy/goproxy/commit/9dafb313348fc3d9e94e9b202c206df0bbc9fa69))
* remove `Goproxy.DisableNotFoundLog` ([cc9448f](https://github.com/goproxy/goproxy/commit/cc9448f870be7966a8686236e6ac1287a8da93c0))
* remove `Goproxy.logErrorf` ([cf3ab39](https://github.com/goproxy/goproxy/commit/cf3ab3925084b52aa81abf21f6623ee16e82b44d))
* remove `Goproxy.PathPrefix` ([3f1a56d](https://github.com/goproxy/goproxy/commit/3f1a56d988aa39edb30777f79e4bf4dca71d9a1c))
* remove `httpDo` ([464303c](https://github.com/goproxy/goproxy/commit/464303cf0eefd85bc20af093d348bb0263578f1f))
* remove `parseRawURL` ([f1c5969](https://github.com/goproxy/goproxy/commit/f1c596954a1eeb58c83230cce5188aea55ebdb7d))
* remove `stringSliceContains` ([621f0e8](https://github.com/goproxy/goproxy/commit/621f0e8d3c9ee4dcd9c17c26489faac635a8b8cd))
* remove default value of `Goproxy.ProxiedSUMDBNames` ([93d07b5](https://github.com/goproxy/goproxy/commit/93d07b5deef8cb8ee2d036878a6c5a56fbb049a1))
* remove mapstructure tag for `Goproxy.Cacher` ([0da063f](https://github.com/goproxy/goproxy/commit/0da063f34d2b72497556b84b625b82ef547a5221))
* remove mapstructure tag support for `Goproxy` ([ff5560f](https://github.com/goproxy/goproxy/commit/ff5560f8d2a47384fcfed370c46fec0b4efec26b))
* remove needless `cachers.GCS.ProjectID` ([3b9b9d5](https://github.com/goproxy/goproxy/commit/3b9b9d5ae86341c609a6f80c4fd40a80cb6afc3f))
* remove unnecessary conversions for IDNs ([52f9021](https://github.com/goproxy/goproxy/commit/52f902107e8c90be264a881d33d1bc24b9cde2fd))
* rename `Cacher.Set` to `Cacher.Put` ([2f24fa5](https://github.com/goproxy/goproxy/commit/2f24fa5506811eb063710e3338ffd661dd41287b))
* rename `exponentialBackoffSleep` to `backoffSleep` ([bc483ec](https://github.com/goproxy/goproxy/commit/bc483ec5076ecbc23799057c38bed4f76c324a47))
* rename `fetch.name` to `fetch.target` ([891ee6f](https://github.com/goproxy/goproxy/commit/891ee6f3ce0a093794ed15ce341d09c77869359e))
* rename `fetchOpsResolve` to `fetchOpsQuery` ([b8d4ba1](https://github.com/goproxy/goproxy/commit/b8d4ba103a78330e665c02a7baaba1790e75beeb))
* rename `Goproxy.GoBinEnv` to `Goproxy.Env` ([cf89854](https://github.com/goproxy/goproxy/commit/cf898541d67911f28c24545984206806bba720fb))
* rename `Goproxy.GoBinMaxWorkers` to `Goproxy.MaxDirectFetches` ([850ca07](https://github.com/goproxy/goproxy/commit/850ca073ef1d5ec8441ebe506738fc45db3ed83c))
* rename `Goproxy.load` and `sumdbClientOps.load` ([c55bfa8](https://github.com/goproxy/goproxy/commit/c55bfa8a6ef7e41d5dabcb8b4d6aa906f5b93e7a))
* rename `Goproxy.MaxGoBinWorkers` to `Goproxy.GoBinMaxWorkers` ([42fa1c1](https://github.com/goproxy/goproxy/commit/42fa1c1432fd0645f9d30abb588fb2dde48891fe))
* rename `Goproxy.SupportedSUMDBNames` to `Goproxy.ProxiedSUMDBNames` ([1e7c4fe](https://github.com/goproxy/goproxy/commit/1e7c4fea74d32fba1ee448f70c6e2aca5ba07719))
* rename `LocalCache` to `localCache` ([052e512](https://github.com/goproxy/goproxy/commit/052e512e4006a6f5021aa3c3de30f6079d4fd835))
* rename `LocalCacher` to `DiskCacher` ([1841bc9](https://github.com/goproxy/goproxy/commit/1841bc926144e136f618f4901c2d1397c51728b8))
* rename `walkGOPROXY` to `walkEnvGOPROXY` ([5bc6617](https://github.com/goproxy/goproxy/commit/5bc66173f83a8c7c87bee1d18d39ab7ef8b61468))
* rename package `cachers` to `cacher` ([7352779](https://github.com/goproxy/goproxy/commit/73527794a13a5b96cdf2495811f2a38cae4b059c))
* reorganize code ([8b5487b](https://github.com/goproxy/goproxy/commit/8b5487bef7ccb6668e4ed8cac6d4d97a49671088))
* replace `errNotFound` with `fs.ErrNotExist` ([8fc9661](https://github.com/goproxy/goproxy/commit/8fc966117534f6b23fed0b47b754a90449bed3ab))
* replace `globsMatchPath` with `module.MatchPrefixPatterns` ([ca2ed62](https://github.com/goproxy/goproxy/commit/ca2ed62635e058231522b4aeb46a3de578e4e0ae))
* replace `interface{}` with `any` ([c22d2a1](https://github.com/goproxy/goproxy/commit/c22d2a1cec1536307541efb8d348928bdafc8f54))
* replace `io.ReadSeekCloser` and `io.NopCloser` ([1c28b63](https://github.com/goproxy/goproxy/commit/1c28b635c936468286a19aa052d2d5f47e1fea75))
* respect system GOPATH when trying direct ([ac1b4e9](https://github.com/goproxy/goproxy/commit/ac1b4e9c51fe8f75581537d0cb348a544ad9058e))
* return error for "off" in `walkEnvGOPROXY` ([82ff8e5](https://github.com/goproxy/goproxy/commit/82ff8e5427c9f4b055e32fb5beda749217d5e541))
* simplify `cachers.GCS.load` ([d71999b](https://github.com/goproxy/goproxy/commit/d71999bb3ded807966112ce1fa52c6d80752d8bf))
* simplify `checkInfoFile` ([8ceb484](https://github.com/goproxy/goproxy/commit/8ceb4849b30acb6787ea09050eee861ca453597f))
* simplify `executeGoCommand` ([0732f05](https://github.com/goproxy/goproxy/commit/0732f05edbfcdd7c9c8015fc8ac2b5d7542f7df3))
* simplify `Goproxy.ServeHTTP` ([94e486c](https://github.com/goproxy/goproxy/commit/94e486c6928c8f5cf35c157512ba9ca33496c411))
* simplify `isTimeoutError` ([32244c2](https://github.com/goproxy/goproxy/commit/32244c2c4fb326ebd69ef60f0511ddfa0490fcee))
* simplify `notFoundError` ([713a36b](https://github.com/goproxy/goproxy/commit/713a36bb6358883485abffe85ec534b6b19157a4))
* simplify `responseModError` ([6a9d266](https://github.com/goproxy/goproxy/commit/6a9d266f73fa96d81c42037bc4a8979d7dfd7a6c))
* simplify `responseNotFound` ([3c56c7e](https://github.com/goproxy/goproxy/commit/3c56c7e214a9f5deb1aecef870dae0483a727fd2))
* simplify caching ([384fb65](https://github.com/goproxy/goproxy/commit/384fb65960cdc4dff154c4150233fe18f9c70de6))
* simplify checksum calculation for `cacher.MinIO` ([bb1be3b](https://github.com/goproxy/goproxy/commit/bb1be3b84e92afa97f0f8000b63db9effbf98e8d))
* simplify code ([274c9fd](https://github.com/goproxy/goproxy/commit/274c9fd7391cac164452e49d553b3d361cf22081))
* simplify code ([7db1451](https://github.com/goproxy/goproxy/commit/7db1451ce59606efaf611ad6794c0455cc1236f9))
* simplify code ([3239f70](https://github.com/goproxy/goproxy/commit/3239f70bbcbb338f6d9f6e7416f81721910eafce))
* simplify code ([8b22ef8](https://github.com/goproxy/goproxy/commit/8b22ef89a5313b71d521bc1081a63aa420f951cf))
* simplify code ([b21e311](https://github.com/goproxy/goproxy/commit/b21e3112b7d353ba22ceeed1d46049d00c53c959))
* simplify code ([a27e528](https://github.com/goproxy/goproxy/commit/a27e528ba19055ec7e882eeaeb2a59f56616e45d))
* simplify error handling in `Goproxy.ServeHTTP` ([91d74da](https://github.com/goproxy/goproxy/commit/91d74da4534710acbc9e4f4c1be6e2e46289a3c8))
* simplify error logging ([361e4bb](https://github.com/goproxy/goproxy/commit/361e4bb16ca1f175d1b9068257e75fa016228880))
* simplify implementation of `sumdbClientOps` ([77f032a](https://github.com/goproxy/goproxy/commit/77f032a2facb9c73f468912de0dd093a2d379e9d))
* simplify info marshalling ([f13f333](https://github.com/goproxy/goproxy/commit/f13f333a8466e5b00c1799808abbfb74011691a6))
* simplify response funcs ([3b528a6](https://github.com/goproxy/goproxy/commit/3b528a691117bbce0a29b0fc60a5963a142b41bb))
* sort `Goproxy`'s fields ([fe2aef0](https://github.com/goproxy/goproxy/commit/fe2aef0915912c0bfb0a6dfce9d3708b599b92a6))
* speed up info requests ([f4454ad](https://github.com/goproxy/goproxy/commit/f4454ad0980224c69824b464fa4a794bb88ab117))
* split `Goproxy.serveFetch` based on fetch operation ([c963f28](https://github.com/goproxy/goproxy/commit/c963f28126d05ae503c952a06d6d8018dfa3ce60))
* standardize abbreviation for checksum database ([a7c7c1d](https://github.com/goproxy/goproxy/commit/a7c7c1dd4fe5fe03d520772f3b94fa18267b8c12))
* supplement mapstructure support for `Goproxy.GoBinEnv` ([0679e17](https://github.com/goproxy/goproxy/commit/0679e1750b9a5f1cb9aa1c3671460a8605251084))
* take care of Last-Modified and ETag response headers ([dd5d22d](https://github.com/goproxy/goproxy/commit/dd5d22d1db39af752597594891b6ae320270904e))
* tidy code ([f5a3cb6](https://github.com/goproxy/goproxy/commit/f5a3cb608f79d798780250bc4dea0da0db5be069))
* unify `appendURL` usages ([ca6aa17](https://github.com/goproxy/goproxy/commit/ca6aa17cee543a85857f907da5b45c0db9fd47d7))
* update `modOutputNotFoundKeywords` ([abcc7e0](https://github.com/goproxy/goproxy/commit/abcc7e04f3a42749f7fbd79f9f378df25e51b9b9))
* update `redactedURL` to Go 1.15 style ([36990d9](https://github.com/goproxy/goproxy/commit/36990d929f0f2f43dac50ac7a3878df44de27aff))
* update `regModuleVersionNotFound` ([0fbe7f5](https://github.com/goproxy/goproxy/commit/0fbe7f5c83f1e3437602950a8c04dc84eb4fbcce))
* update `regModuleVersionNotFound` ([94c1b39](https://github.com/goproxy/goproxy/commit/94c1b3918dbdeda9f3b3be61e20720f15f6c3d62))
* update `regModuleVersionNotFound` ([d488850](https://github.com/goproxy/goproxy/commit/d488850987e3ce5e9548b6bf5f4336b0e83669dd))
* update `regModuleVersionNotFound` ([7d12f3b](https://github.com/goproxy/goproxy/commit/7d12f3b594cbed340a6420c57bf040b8e39bc06a))
* use `Goproxy.httpClient` to connect to outside ([b266aa5](https://github.com/goproxy/goproxy/commit/b266aa5b797e52d21f8174308919ea210e776a9f))
* use `Goproxy.logError` to simplify code ([ad2d878](https://github.com/goproxy/goproxy/commit/ad2d87854cb876d969a76d083690eb980a68863b))
* use `httpGet` to simplify code ([d55fa5f](https://github.com/goproxy/goproxy/commit/d55fa5f151b530eb023fd261f28be92528d0a3d2))
* use custom metadata to store cache's checksum ([161cd44](https://github.com/goproxy/goproxy/commit/161cd44d741849b7d139c104db4b0330a000f86e))
* use github.com/minio/minio-go/v7 ([1bbac0a](https://github.com/goproxy/goproxy/commit/1bbac0a95689fe7bd57fbb7a8fe4664f1b1579a8))
* use Go 1.12 ([bb82924](https://github.com/goproxy/goproxy/commit/bb82924a90b8aa22c07ef8f080a67db07eaed52d))
* use Go 1.13 ([1618363](https://github.com/goproxy/goproxy/commit/161836338aff2348a5dc11daeccbeb4b4f8179e5))
* use Go 1.20 for releases ([bf9a707](https://github.com/goproxy/goproxy/commit/bf9a707d2d4af31ec450806f518dba346744abe6))
* use Go 1.21 for releases ([ac045fb](https://github.com/goproxy/goproxy/commit/ac045fbe37634b39fe0f6301dbbd15ff35bc0759))
* use Go 1.22 for releases ([6edc511](https://github.com/goproxy/goproxy/commit/6edc5117192e65caa00aae16e10933c9f6e7bf96))
* use minio/minio-go to simplify `cachers.GCS` ([09c1d80](https://github.com/goproxy/goproxy/commit/09c1d800c4274b449d87860e1ea0f5c3205410ac))
* use minio/minio-go to simplify `cachers.Kodo` ([dd540a0](https://github.com/goproxy/goproxy/commit/dd540a0a6b820103fd368dbd400e78987861dae6))
* use minio/minio-go to simplify `cachers.OSS` ([a89862f](https://github.com/goproxy/goproxy/commit/a89862f32011e279495b925e6a756811c926c828))
* use minio/minio-go to simplify implementations of `Cacher` ([a32cf49](https://github.com/goproxy/goproxy/commit/a32cf49ab4ffd3612ebc8ab23f91c49f64eaaab0))
* use path style endpoint for `cacher.Kodo` ([d5a02f0](https://github.com/goproxy/goproxy/commit/d5a02f043d6b5567c78da88c9f355dc2f99d3b78))
* utilize `semver.Sort` ([7ca72cc](https://github.com/goproxy/goproxy/commit/7ca72cc5685cc2253d4efc053c1a5027f0e19ce6))
* utilize `slices` package from stdlib ([#86](https://github.com/goproxy/goproxy/issues/86)) ([b108687](https://github.com/goproxy/goproxy/commit/b108687b51813c7110fde0b6309876f278f6e09a))
* utilize `strings.Cut` ([a32d730](https://github.com/goproxy/goproxy/commit/a32d730447a6e4020eefa9ddd9078341397a33a9))
* utilize Go 1.22 for-range over integers ([#117](https://github.com/goproxy/goproxy/issues/117)) ([50d2fc6](https://github.com/goproxy/goproxy/commit/50d2fc6edadc7feb3d2f5a2414e6bee440574df0))


### Tests

* add cacher_test.go ([dce1533](https://github.com/goproxy/goproxy/commit/dce15334c6a7fd86a2a9bff4ed5293b1c59444b1))
* add goproxy_test.go ([395583c](https://github.com/goproxy/goproxy/commit/395583c20ea3b5750ca75430a52ba9911f64796b))
* add response_test.go ([c324b34](https://github.com/goproxy/goproxy/commit/c324b345122180dda5786dfb0439dd7771c03887))
* avoid passing nil `context.Context` ([cb855d3](https://github.com/goproxy/goproxy/commit/cb855d3d850f65be5a02b5fabcf0b44d9c50a19a))
* avoid using deprecated `httptest.ResponseRecorder.HeaderMap` ([e1656ea](https://github.com/goproxy/goproxy/commit/e1656eae15a7858cfe269194b2920e3b14768089))
* correct ineffectual assignments ([ec6395a](https://github.com/goproxy/goproxy/commit/ec6395a017a7f764f5749e70cfb9b7d29f83c491))
* correct use of `%v` format verb ([c899635](https://github.com/goproxy/goproxy/commit/c8996358abb6f8ec8716803cebd0030a8e65bb23))
* cover `fetch.doProxy` ([93f8818](https://github.com/goproxy/goproxy/commit/93f88188db4eb2941776ab8ac50f30cbe82d0b49))
* cover `fetch` ([789087b](https://github.com/goproxy/goproxy/commit/789087b0fbbd637595c50ba9d54e0d4d6d26a221))
* cover `fetchResult` ([1788991](https://github.com/goproxy/goproxy/commit/1788991e2d751a924bb7207958960733e8411b26))
* cover `Goproxy.load`, `newFetch` and `fetchOps` ([ab92faa](https://github.com/goproxy/goproxy/commit/ab92faaca91de5c0a098719161a06f7238c7c036))
* cover `Goproxy` ([31008d9](https://github.com/goproxy/goproxy/commit/31008d9cb2273d9a88865c2339b60a5f3a8e1578))
* cover `responseSuccess` ([0790b11](https://github.com/goproxy/goproxy/commit/0790b11fb75fc658dd0ce919e0d473db17dda764))
* cover `sumdbClientOps` ([1ba6787](https://github.com/goproxy/goproxy/commit/1ba67871fb0319b90bfa14ec6e267deb2b5306d0))
* cover Go 1.20 ([bcf1472](https://github.com/goproxy/goproxy/commit/bcf14723d5e650cc56d894c09ff6f1aea8851de3))
* cover Go 1.21 ([a49144b](https://github.com/goproxy/goproxy/commit/a49144bbdb8a98cf9a30903f6a8a53bc31256668))
* cover Go 1.22 ([efd7ed6](https://github.com/goproxy/goproxy/commit/efd7ed65391f9af89e9e53051dfe264b411588a6))
* cover Go 1.23 ([#77](https://github.com/goproxy/goproxy/issues/77)) ([b8da543](https://github.com/goproxy/goproxy/commit/b8da543f31677edc2901aedc8a056477a7949c78))
* cover Go 1.24 ([#96](https://github.com/goproxy/goproxy/issues/96)) ([d93abb4](https://github.com/goproxy/goproxy/commit/d93abb4bd1e107ad6c2369b3114736fca89273de))
* cover module file verification funcs ([b7db93d](https://github.com/goproxy/goproxy/commit/b7db93d20ef8aca5863aef7d6636a7673b16cc22))
* do not double-quote errors in logs ([#116](https://github.com/goproxy/goproxy/issues/116)) ([39e687a](https://github.com/goproxy/goproxy/commit/39e687ac5dc1ebc7cd0f38f785dc0867f84f89e1))
* enrich `TestDirCacher` ([621dd49](https://github.com/goproxy/goproxy/commit/621dd49c5572a11220896269518b2bc140854805))
* fix `TestParseEnvGOSUMDB` for Go 1.18 ([bb8546f](https://github.com/goproxy/goproxy/commit/bb8546fdf0b84ac470b8dde56d9271dde93f0b4b))
* fix data races caused by misuse of `httptest.Server` ([20ac5e6](https://github.com/goproxy/goproxy/commit/20ac5e6bab0ec1983ae061d7708c14c98b322236))
* improve error matching ([38e4c50](https://github.com/goproxy/goproxy/commit/38e4c5066dcd9f0cc8b1be26a7b163dc8c8dd4c2))
* improve test organization with subtests ([#110](https://github.com/goproxy/goproxy/issues/110)) ([5b2a4c8](https://github.com/goproxy/goproxy/commit/5b2a4c8ed731815ae519b8097987dbd62c99cbbe))
* make only local network connections during testing ([439cb26](https://github.com/goproxy/goproxy/commit/439cb266e3fde4ddbb4ee378b4c4e514b48e59aa))
* no longer use github.com/stretchr/testify/assert ([2b25c66](https://github.com/goproxy/goproxy/commit/2b25c66697b99ade49f97f1ac6bd131580bdaf0c))
* update goproxy_test.go ([bfc1938](https://github.com/goproxy/goproxy/commit/bfc193898d26d5f39fa6e5f84ff9955d9e5a3400))
* use loops to make tests more maintainable ([79c32ca](https://github.com/goproxy/goproxy/commit/79c32ca06a1425342712d587cfcce7da46588534))
* utilize `testing.T.TempDir` ([a37afd3](https://github.com/goproxy/goproxy/commit/a37afd302d7a6bfc4984da823160d1f365bee491))


### Documentation

* adapt to Go 1.13 ([c7ec04e](https://github.com/goproxy/goproxy/commit/c7ec04e4dda06d763a54e5909d1c4ccc1cc722ec))
* **cmd/goproxy:** mention `server`'s dependence on Go and VCS binaries ([afa4ab7](https://github.com/goproxy/goproxy/commit/afa4ab7d7616d2cc6e1f73659c6d9ea66da0c9db))
* correct comments ([f9b8de3](https://github.com/goproxy/goproxy/commit/f9b8de304524516642a4823fbc89f5191fc0434f))
* correct nil value comment of `Goproxy.Cacher` ([0dde1a5](https://github.com/goproxy/goproxy/commit/0dde1a5f8927473dc68f667d793a46b64acd629a))
* improve comments ([b42d473](https://github.com/goproxy/goproxy/commit/b42d473c010f1ac8c9052c1da7c9d743ee59cf64))
* improve comments ([21be5e3](https://github.com/goproxy/goproxy/commit/21be5e316838aedb1e3963221b6012cb2042137c))
* improve comments for `Goproxy.GoBinName` and `Goproxy.Transport` ([7ed2183](https://github.com/goproxy/goproxy/commit/7ed21835464268726d814311ae6eded356453bb3))
* improve comments of `Goproxy` ([cff7144](https://github.com/goproxy/goproxy/commit/cff7144e465b93cedd78290e2dd9218c80dde300))
* improve optional interface comment of `Cacher.Get` ([702fcb1](https://github.com/goproxy/goproxy/commit/702fcb1c265076df1f748ed921ef597fa8ea3947))
* mention Disable-Module-Fetch header support ([7622060](https://github.com/goproxy/goproxy/commit/76220605d8d495a740316077d712f97cd5bd2f24))
* mention special treatment for `fs.ErrNotExist` ([7ae1b7b](https://github.com/goproxy/goproxy/commit/7ae1b7b2da031d68d7bae6dada4e913284f37902))
* mention that misuse of a shared `GOMODCACHE` may lead to deadlocks ([365edeb](https://github.com/goproxy/goproxy/commit/365edeb2a5943fec1a23a2d9efdcbfb905106e36)), closes [#44](https://github.com/goproxy/goproxy/issues/44)
* **README.md:** add Conventional Commits requirement to "Contributing" section ([#81](https://github.com/goproxy/goproxy/issues/81)) ([c0ce09d](https://github.com/goproxy/goproxy/commit/c0ce09d6e384a61f7f012589da508f3d48cd738b))
* refine comments ([fa3993b](https://github.com/goproxy/goproxy/commit/fa3993b96edc7141c1471560b5853269439c564a))
* remove minimum Go version comment for `Goproxy.GoBinName` ([d6c2e4c](https://github.com/goproxy/goproxy/commit/d6c2e4c6e1b2ff7f2dc880beb516ba76a1d5d163))
* replace golang.org with go.dev ([4ce1139](https://github.com/goproxy/goproxy/commit/4ce11393b8af2883607fea697cc4e6a8a017574c))
* update comments to Go 1.19 style ([326601b](https://github.com/goproxy/goproxy/commit/326601be9367a1722254807ade72e03baef612c7))


### Miscellaneous Chores

* **.goreleaser.yaml:** add DOCKER_IMAGE_REPO for dynamic repo config ([#104](https://github.com/goproxy/goproxy/issues/104)) ([524fde2](https://github.com/goproxy/goproxy/commit/524fde25a2a7c41037201f63942ad0d1bb60fa72))
* **.goreleaser.yaml:** align GORELEASER_ARTIFACTS_TARBALL with archive name template ([#89](https://github.com/goproxy/goproxy/issues/89)) ([fe067ab](https://github.com/goproxy/goproxy/commit/fe067abab77dcfa2a0caefb42adc01714f66eb03))
* add .github/FUNDING.yaml ([d1785d8](https://github.com/goproxy/goproxy/commit/d1785d8cd0ad552e6aac614d5d63a6f0e031722d))
* add .github/workflows/main.yml ([ab950bc](https://github.com/goproxy/goproxy/commit/ab950bc23efad0ecc6f9e6055b28dbf2e7cf57b3))
* add .github/workflows/release.yml ([cc20b1e](https://github.com/goproxy/goproxy/commit/cc20b1e60f54e6d959a7912c530cb0a272ea5158))
* add CODE_OF_CONDUCT.md ([69382c2](https://github.com/goproxy/goproxy/commit/69382c2c26c8a37be9743af415a914c3b6308a64))
* add codecov badge to README.md ([a215b3d](https://github.com/goproxy/goproxy/commit/a215b3d90752f10bc9be8a98feae3094e50cf312))
* add Docker image support ([27501d9](https://github.com/goproxy/goproxy/commit/27501d9c0f711c562a65ff3fbf8179a339ef1c0f)), closes [#36](https://github.com/goproxy/goproxy/issues/36)
* add supported VCS packages to Docker image ([5b2c60e](https://github.com/goproxy/goproxy/commit/5b2c60ee91864ffc4e9ef2f32f99b08946f9d1ef))
* bump `.goreleaser.yaml` to v2 ([#75](https://github.com/goproxy/goproxy/issues/75)) ([7a75593](https://github.com/goproxy/goproxy/commit/7a75593fc37b82406c3db882bb864dbeb4ebc60c))
* bump actions/setup-go and improve usage of docker/metadata-action ([89f857b](https://github.com/goproxy/goproxy/commit/89f857b6f66f6052409443ee7784abce67559b39))
* bump all GitHub Actions ([f25159e](https://github.com/goproxy/goproxy/commit/f25159e7d5652f3880a879be9d152fb76c35fa54))
* bump codecov/codecov-action from v3 to v4 ([7becc8d](https://github.com/goproxy/goproxy/commit/7becc8d11f588fd55b2054b7ab7c9fd79269ff6d))
* bump github.com/minio/minio-go/v7 ([0038324](https://github.com/goproxy/goproxy/commit/00383240cfd3c60d0e16713c9dc73a697481bbf5))
* bump github.com/minio/minio-go/v7 ([a9e5e2d](https://github.com/goproxy/goproxy/commit/a9e5e2dfb3ac1108113cb77773790a04d97c710c))
* bump golang.org/x/mod ([fa185c0](https://github.com/goproxy/goproxy/commit/fa185c04b24b87d74432817e2a597dfaaaaf538a))
* bump golang.org/x/mod ([181001f](https://github.com/goproxy/goproxy/commit/181001fe44c36554294b6e8c8e66cb46b0dd8cd1))
* bump golang.org/x/mod and github.com/spf13/cobra ([88c8535](https://github.com/goproxy/goproxy/commit/88c85357adfa5e8f2fee30236b5c0c9ffbdd1391))
* bump version of all GitHub Actions ([f3c0bb0](https://github.com/goproxy/goproxy/commit/f3c0bb0652793ea86eb16bdd422ad26fb27cefae))
* **ci:** add build tests ([#105](https://github.com/goproxy/goproxy/issues/105)) ([92cac17](https://github.com/goproxy/goproxy/commit/92cac178cff71655131c3a27948fc92a6aeb7b43))
* **ci:** add support for `linux/riscv64` ([#94](https://github.com/goproxy/goproxy/issues/94)) ([cd425f3](https://github.com/goproxy/goproxy/commit/cd425f3907ea549342253d2fc08bdfa16382b265))
* **ci:** bump codecov/codecov-action from 4 to 5 ([#91](https://github.com/goproxy/goproxy/issues/91)) ([ab618b0](https://github.com/goproxy/goproxy/commit/ab618b0b09f0b9c1f4c80b5a00a7cc37d56f2666))
* **ci:** bump goreleaser/goreleaser-action from 5 to 6 ([#64](https://github.com/goproxy/goproxy/issues/64)) ([afa0f0b](https://github.com/goproxy/goproxy/commit/afa0f0b561da1dd88f9d96aef338df3ec5b6eb1c))
* **ci:** configure release-please to open PRs as drafts ([#83](https://github.com/goproxy/goproxy/issues/83)) ([320a8c1](https://github.com/goproxy/goproxy/commit/320a8c17837c44373511372ac3750bb5d8b25bfe))
* **ci:** fix usage of codecov/codecov-action@v5 ([#108](https://github.com/goproxy/goproxy/issues/108)) ([bbf1666](https://github.com/goproxy/goproxy/commit/bbf1666550b726d7ab68a72fd62bcd6db6b6fe37))
* **ci:** format .github/workflows/test.yaml ([#51](https://github.com/goproxy/goproxy/issues/51)) ([37c723d](https://github.com/goproxy/goproxy/commit/37c723d4752a35af665e1b56f90aa492100b754f))
* **ci:** utilize googleapis/release-please-action@v4 ([#62](https://github.com/goproxy/goproxy/issues/62)) ([f2383d6](https://github.com/goproxy/goproxy/commit/f2383d6d93aeb5ed8a7528e1b0076ac7f09276e9))
* correct setup-qemu-action version in release action ([35159ac](https://github.com/goproxy/goproxy/commit/35159acc43bcf7428bcfbf055b4363c3ae6bed92))
* cover Go 1.18 ([f5d02ed](https://github.com/goproxy/goproxy/commit/f5d02ed5c89a03cdbdda88efad9d66a79ff0e279))
* cover Go 1.19 ([926d785](https://github.com/goproxy/goproxy/commit/926d7850bccb6a9b29e8f002a044a7cfa717b352))
* **deps:** bump github.com/minio/minio-go/v7 from 7.0.88 to 7.0.91 ([#119](https://github.com/goproxy/goproxy/issues/119)) ([20411bd](https://github.com/goproxy/goproxy/commit/20411bdc6771f074785c7226310d0dc99d40ea21))
* **deps:** bump github.com/spf13/cobra from 1.8.0 to 1.8.1 ([#65](https://github.com/goproxy/goproxy/issues/65)) ([39a876c](https://github.com/goproxy/goproxy/commit/39a876c6e55b84f77ebcab792bf7e1ea85a58022))
* **deps:** bump golang.org/x/crypto from 0.14.0 to 0.17.0 ([#49](https://github.com/goproxy/goproxy/issues/49)) ([e1e9cf1](https://github.com/goproxy/goproxy/commit/e1e9cf1261f8deaa23f772bdf67532eb86365c4f))
* **deps:** bump golang.org/x/crypto from 0.28.0 to 0.31.0 ([#92](https://github.com/goproxy/goproxy/issues/92)) ([6014fda](https://github.com/goproxy/goproxy/commit/6014fda90cce0891c9f11ab044ed7e6c66acdf09))
* **deps:** bump golang.org/x/crypto from 0.33.0 to 0.35.0 ([#113](https://github.com/goproxy/goproxy/issues/113)) ([ad197dd](https://github.com/goproxy/goproxy/commit/ad197dd3f86075a5aad25a22d6313a97f92247d8))
* **deps:** bump golang.org/x/mod and github.com/minio/minio-go/v7 ([#123](https://github.com/goproxy/goproxy/issues/123)) ([4eee47d](https://github.com/goproxy/goproxy/commit/4eee47de7c80acba9f3138f4a6400a54fe62c987))
* **deps:** bump golang.org/x/mod and github.com/minio/minio-go/v7 ([#85](https://github.com/goproxy/goproxy/issues/85)) ([f44b882](https://github.com/goproxy/goproxy/commit/f44b8827e37dd3636606a42649af7d3750ecc6e3))
* **deps:** bump golang.org/x/mod from 0.16.0 to 0.18.0 ([#66](https://github.com/goproxy/goproxy/issues/66)) ([b4c1099](https://github.com/goproxy/goproxy/commit/b4c1099bf0ef93f953abff554eaae979343ee2cf))
* **deps:** bump golang.org/x/mod from 0.18.0 to 0.19.0 ([#68](https://github.com/goproxy/goproxy/issues/68)) ([141fb73](https://github.com/goproxy/goproxy/commit/141fb73d2e6055df46cb99df1b0ac6fba1b15090))
* **deps:** bump golang.org/x/mod, github.com/spf13/cobra, github.com/minio/minio-go/v7 ([#98](https://github.com/goproxy/goproxy/issues/98)) ([e75760c](https://github.com/goproxy/goproxy/commit/e75760c27ff1a22cda603f83b324cce8c3d9f5bc))
* **deps:** bump golang.org/x/net from 0.14.0 to 0.17.0 ([#47](https://github.com/goproxy/goproxy/issues/47)) ([bc65434](https://github.com/goproxy/goproxy/commit/bc65434c7815b7af9ecc6c6320da4859a9285e0c))
* **deps:** bump golang.org/x/net from 0.19.0 to 0.23.0 ([#54](https://github.com/goproxy/goproxy/issues/54)) ([80087f0](https://github.com/goproxy/goproxy/commit/80087f057b12f018827a083ab29dd0cf1bce1789))
* **deps:** bump golang.org/x/net from 0.30.0 to 0.33.0 ([#93](https://github.com/goproxy/goproxy/issues/93)) ([093e27c](https://github.com/goproxy/goproxy/commit/093e27cfad43eb5d6ba0b6ecccc7a2edb23045d3))
* **deps:** bump golang.org/x/net from 0.35.0 to 0.38.0 ([#115](https://github.com/goproxy/goproxy/issues/115)) ([239fd87](https://github.com/goproxy/goproxy/commit/239fd8753c2e9b0564d57f123fa721085e330da3))
* **Dockerfile:** modify artifact handling to use USE_GORELEASER_ARTIFACTS build arg ([#50](https://github.com/goproxy/goproxy/issues/50)) ([97b6cc1](https://github.com/goproxy/goproxy/commit/97b6cc1b1adb1fa896d053be4f1659d837fd57a6))
* **Dockerfile:** use `golang:1.24-alpine3.21` as base image ([#102](https://github.com/goproxy/goproxy/issues/102)) ([bae1a73](https://github.com/goproxy/goproxy/commit/bae1a7314993814495056b48629975cd5178c27f))
* **Dockerfile:** use Alpine 3.20 as base image ([#59](https://github.com/goproxy/goproxy/issues/59)) ([4754c19](https://github.com/goproxy/goproxy/commit/4754c198e5b0752d25d23e785f2309958437d416))
* **Dockerfile:** use Alpine 3.21 as base image ([#84](https://github.com/goproxy/goproxy/issues/84)) ([7bb9dfd](https://github.com/goproxy/goproxy/commit/7bb9dfd090ab4faa4f0abccc65f0abdbef942542))
* fix default `GOPATH` directory for Docker image ([9d23ab9](https://github.com/goproxy/goproxy/commit/9d23ab99672a278afa3c13939ef50629e0bb2f1b))
* fix Docker image release ([2222ccc](https://github.com/goproxy/goproxy/commit/2222ccc1932bfdc89b95f6d3894d8f8ebc4fe049))
* format .github/workflows/release.yml ([93c07e4](https://github.com/goproxy/goproxy/commit/93c07e443f39416e47ecd618a94ff6d252a8a538))
* format README.md ([#61](https://github.com/goproxy/goproxy/issues/61)) ([0d2f7d6](https://github.com/goproxy/goproxy/commit/0d2f7d666a486ba7741fd3e39480dc9722a85e6b))
* improve "Quick Start" section in README.md ([54b6f5c](https://github.com/goproxy/goproxy/commit/54b6f5c4ff3840e40f3e2aa12e90ef1928067ba4))
* improve Docker image release ([dc60bf3](https://github.com/goproxy/goproxy/commit/dc60bf382974f3a7f1adcba16dd9133442648aca))
* improve Docker image with default USER, WORKDIR, and listening address ([0c42a7c](https://github.com/goproxy/goproxy/commit/0c42a7ce3524b746199fd661d79a4d566f6dc377))
* improve README.md ([b07aef6](https://github.com/goproxy/goproxy/commit/b07aef6de778aae2c77b0c1d1570fb2a9b07b5bb))
* inject version info at build time for Docker image ([480af60](https://github.com/goproxy/goproxy/commit/480af60a5f56690c43775b613100e68f5b7e84a1))
* **master:** release 0.17.0 ([#63](https://github.com/goproxy/goproxy/issues/63)) ([170446b](https://github.com/goproxy/goproxy/commit/170446bd2e77486417d0b3fca6ddbb0c8d34edd9))
* **master:** release 0.17.1 ([#69](https://github.com/goproxy/goproxy/issues/69)) ([4a56f64](https://github.com/goproxy/goproxy/commit/4a56f64797a33878608c5fd9920ef755d6d3a8d5))
* **master:** release 0.17.2 ([#71](https://github.com/goproxy/goproxy/issues/71)) ([a0024b0](https://github.com/goproxy/goproxy/commit/a0024b0724a1f0bb4be7c806af091eebadddd377))
* **master:** release 0.18.0 ([#76](https://github.com/goproxy/goproxy/issues/76)) ([bf40789](https://github.com/goproxy/goproxy/commit/bf40789d255ad75a5267284ce84880c3ad38cfda))
* **master:** release 0.18.1 ([#82](https://github.com/goproxy/goproxy/issues/82)) ([bf3c3be](https://github.com/goproxy/goproxy/commit/bf3c3bec11d5f92861d2257131b19f54f1ac93a9))
* **master:** release 0.18.2 ([#88](https://github.com/goproxy/goproxy/issues/88)) ([d3cef02](https://github.com/goproxy/goproxy/commit/d3cef02481ce233a0a4b109b274c3ff7170369f0))
* **master:** release 0.19.0 ([#90](https://github.com/goproxy/goproxy/issues/90)) ([a9d238b](https://github.com/goproxy/goproxy/commit/a9d238b5ffe61b35d8841aad4d7151fbed1d7cda))
* **master:** release 0.19.1 ([#101](https://github.com/goproxy/goproxy/issues/101)) ([b603608](https://github.com/goproxy/goproxy/commit/b60360827c3e2255a5b7caaaca8783fe21638130))
* **master:** release 0.19.2 ([#103](https://github.com/goproxy/goproxy/issues/103)) ([7973135](https://github.com/goproxy/goproxy/commit/7973135a611b48d4173d4ad40342d8bf2f9240e6))
* **master:** release 0.20.0 ([#107](https://github.com/goproxy/goproxy/issues/107)) ([2a6f454](https://github.com/goproxy/goproxy/commit/2a6f454edfbf43f213e9ecf14e16ab5b939cb5d1))
* **master:** release 0.20.1 ([#114](https://github.com/goproxy/goproxy/issues/114)) ([271d439](https://github.com/goproxy/goproxy/commit/271d439f0d56f8538f1d9c6840b008d736f2cc11))
* **master:** release 0.20.2 ([#124](https://github.com/goproxy/goproxy/issues/124)) ([8711d08](https://github.com/goproxy/goproxy/commit/8711d08b5fc81c1d8d23ba04f1a2057beaa6a522))
* merge pull request [#1](https://github.com/goproxy/goproxy/issues/1) from vrischmann/fix ([5279985](https://github.com/goproxy/goproxy/commit/52799853012f1468d1fc2ce19575d40076aa7606))
* merge pull request [#2](https://github.com/goproxy/goproxy/issues/2) from vrischmann/fix-silent-errors ([9d61d9b](https://github.com/goproxy/goproxy/commit/9d61d9b1fafdc963ee62e63dcad233acbf1ca13e))
* merge pull request [#5](https://github.com/goproxy/goproxy/issues/5) from lwalen/patch-1 ([ac2fc90](https://github.com/goproxy/goproxy/commit/ac2fc9088edbecaa72e364a4d37dc662d1227c60))
* prefer `owner/repo` from GHA over `goproxy/goproxy` for ghcr.io ([5ea55a5](https://github.com/goproxy/goproxy/commit/5ea55a51aadd6d3ae1600af8b4af7b044b5c8d51))
* release 0.17.0 ([#67](https://github.com/goproxy/goproxy/issues/67)) ([c688753](https://github.com/goproxy/goproxy/commit/c6887530ee86bbe7195f61af7002b6c358cc354b))
* release 0.17.2 ([#70](https://github.com/goproxy/goproxy/issues/70)) ([5bf903a](https://github.com/goproxy/goproxy/commit/5bf903a6a3509c8607b8c1f9bca92b6fa92eb3ce)), closes [#57](https://github.com/goproxy/goproxy/issues/57)
* release 0.18.0 ([#80](https://github.com/goproxy/goproxy/issues/80)) ([c985dba](https://github.com/goproxy/goproxy/commit/c985dbaa2025098fa1b671f8366122ecc31bbc33))
* release 0.19.0 ([#99](https://github.com/goproxy/goproxy/issues/99)) ([6ea2ff0](https://github.com/goproxy/goproxy/commit/6ea2ff06922eaa0879035ff78e392b3a3fdabb9d))
* relicense project ([51384d9](https://github.com/goproxy/goproxy/commit/51384d9ea31c3198b2b288099655bf0e2727bd4c))
* remove .gitignore ([1ba8405](https://github.com/goproxy/goproxy/commit/1ba8405212dfdd948b45d1acd47406662cb2dadd))
* remove short commit hash as tag for Docker image ([14e06d0](https://github.com/goproxy/goproxy/commit/14e06d0e028cf022cdc477f6ca7d831d9f168dba))
* rename .github/workflows/main.yml ([34aaea2](https://github.com/goproxy/goproxy/commit/34aaea2ed20833b7b67a8cd3d9f054284c7b7b7b))
* rename sumdbclientops.go to sumdb_client_ops.go ([114f135](https://github.com/goproxy/goproxy/commit/114f1358f9db324e1d6ba2f2082f969b45131ee4))
* replace `interface{}` with `any` ([#118](https://github.com/goproxy/goproxy/issues/118)) ([86253b8](https://github.com/goproxy/goproxy/commit/86253b8a97adffeb89151b2799a81a97e7f81ff6))
* set CODECOV_TOKEN for codecov/codecov-action@v4 ([cbcd3b9](https://github.com/goproxy/goproxy/commit/cbcd3b915bd577acfbe9eeae82b5eb51e372897e))
* simplify "Quick Start" section in README.md ([5f62e90](https://github.com/goproxy/goproxy/commit/5f62e90e9d80b437b9625497551c40ee11a5d92b))
* simplify Dockerfile ([4cce0f5](https://github.com/goproxy/goproxy/commit/4cce0f5fca753427974ff1a61ba8ac0b16ac2a2d))
* simplify Dockerfile ([988a4b9](https://github.com/goproxy/goproxy/commit/988a4b9dc0894449ff95469dcdf164fa90baf46f))
* update .github/workflows/main.yml ([9c110cf](https://github.com/goproxy/goproxy/commit/9c110cff373c735b2b6d0a8a220eaf09125bd81a))
* update .github/workflows/main.yml ([d0f2999](https://github.com/goproxy/goproxy/commit/d0f2999dd7c94722657ddad89bdfed4313fb39c2))
* update .github/workflows/main.yml ([a253829](https://github.com/goproxy/goproxy/commit/a253829992d0c0da0c5d903cc1bbc3f244b0639d))
* update .github/workflows/main.yml ([9e719a1](https://github.com/goproxy/goproxy/commit/9e719a18ea6d58555ae3f27e681bb306e3660469))
* update .github/workflows/main.yml ([7aff9ae](https://github.com/goproxy/goproxy/commit/7aff9ae16206430d771d24b366624f0515140793))
* update .github/workflows/main.yml ([5345a66](https://github.com/goproxy/goproxy/commit/5345a66de68136e4afa45d265e529bd529a4acc4))
* update .github/workflows/main.yml ([81cc28c](https://github.com/goproxy/goproxy/commit/81cc28c3db6c2ebd3c9f4ca79f0c9400305ce095))
* update .github/workflows/main.yml ([d9aab3d](https://github.com/goproxy/goproxy/commit/d9aab3dc787fce0c4dda60ba9d42097fdfe7cc19))
* update .github/workflows/main.yml ([4c93725](https://github.com/goproxy/goproxy/commit/4c937254d2f36686c9d446c47b3ee69c9e431a2c))
* update .github/workflows/test.yml ([0009d36](https://github.com/goproxy/goproxy/commit/0009d368a76bd50afe7bb412a3031119fe4caa51))
* update go.sum ([0ea3349](https://github.com/goproxy/goproxy/commit/0ea33499c70cc6001fa98ec967e39838c15115a7))
* update modules ([271218a](https://github.com/goproxy/goproxy/commit/271218a06c5158aee2f02afe04f9ac329cdde600))
* update modules ([312f272](https://github.com/goproxy/goproxy/commit/312f272301f80e3a77b0abb8a21d9fe9355bf6ec))
* update modules ([0b83f27](https://github.com/goproxy/goproxy/commit/0b83f27d39791f6f4aa91bd68ce1b32d430dde80))
* update modules ([3ff6a2b](https://github.com/goproxy/goproxy/commit/3ff6a2b758a147de6c15930d462a599f7b38cec8))
* update modules ([6fb2130](https://github.com/goproxy/goproxy/commit/6fb21302f1f7723b55d9bb012eaa47eaff63ce09))
* update modules ([7db16d8](https://github.com/goproxy/goproxy/commit/7db16d878f3a57c7c25fab125a4f01bd8bc7e0ec))
* update modules ([85c2b42](https://github.com/goproxy/goproxy/commit/85c2b42f600b1650ba37808c91c4e9c72d514242))
* update modules ([43d8e2f](https://github.com/goproxy/goproxy/commit/43d8e2fa277551b89ceeb1136b8b7953da244428))
* update modules ([c94ddec](https://github.com/goproxy/goproxy/commit/c94ddec6aae2bf62ddc5a858d7b0f41a1be7d448))
* update modules ([1e85aeb](https://github.com/goproxy/goproxy/commit/1e85aebed0ef70b53d6dd4565ff357e4558b1088))
* update modules ([32dcf62](https://github.com/goproxy/goproxy/commit/32dcf6238b4e877f8decc7e21bfce74c5710438d))
* update modules ([392c795](https://github.com/goproxy/goproxy/commit/392c795bf4af527f84ffc12b05859f59b35bf937))
* update modules ([99cef39](https://github.com/goproxy/goproxy/commit/99cef394d8e1af24743b1c9929d8af48ae0652ae))
* update modules ([594648f](https://github.com/goproxy/goproxy/commit/594648fa83e39b778e0bb7aba86ce34aa858704b))
* update modules ([a7777fc](https://github.com/goproxy/goproxy/commit/a7777fc9fdef0ac61633d2ee76d0fb3b5bb66047))
* update modules ([7d4a942](https://github.com/goproxy/goproxy/commit/7d4a942372c79f51bd3908380a1639189da14164))
* update modules ([6f8624f](https://github.com/goproxy/goproxy/commit/6f8624f105054b65d96e2985dde271a573703563))
* update modules ([d69a2a0](https://github.com/goproxy/goproxy/commit/d69a2a00f4eb3870c99c2fe7796054af7fd6c36d))
* update modules ([69de332](https://github.com/goproxy/goproxy/commit/69de332d57723c01c0875e73dbebe1d1ed75c913))
* update modules ([5941b87](https://github.com/goproxy/goproxy/commit/5941b87b630eeec92c07b4c12b3d14a9bec2313b))
* update modules ([346948d](https://github.com/goproxy/goproxy/commit/346948ddca9a7fedad9d5c3918adfbbbf35cb026))
* update README.md ([5908145](https://github.com/goproxy/goproxy/commit/5908145cdedeb5a99168e7945e35aa7231e6fa68))
* update README.md ([bcbbfd8](https://github.com/goproxy/goproxy/commit/bcbbfd8db5bc6f7894d45cc33287db16d4405888))
* update README.md ([f36d68f](https://github.com/goproxy/goproxy/commit/f36d68fa1decb8371498587ecdf50a707ed952b7))
* update README.md ([4fec2bb](https://github.com/goproxy/goproxy/commit/4fec2bbf7f70073311d6922b06d1edcbe6e7422d))
* update README.md ([e5db0f7](https://github.com/goproxy/goproxy/commit/e5db0f79370689408e87fd67c83c9e3d56e90285))
* update README.md ([bb92c50](https://github.com/goproxy/goproxy/commit/bb92c50db358e594b7db64d8b1f76a8ce58fa549))
* update README.md ([1b8111e](https://github.com/goproxy/goproxy/commit/1b8111e773cd7ced6c107d087839576056960f4b))
* update README.md ([5dfc789](https://github.com/goproxy/goproxy/commit/5dfc789a57d3896e6c2401f11ba7cd1f159817f7))
* update README.md ([d5f2af3](https://github.com/goproxy/goproxy/commit/d5f2af350d1e15f36c29c45b89deb7bea9f39b8e))
* update README.md ([eda4ad0](https://github.com/goproxy/goproxy/commit/eda4ad0bc81cd9b6ffe29a24264d8cafc6bdc6c7))
* update README.md ([fca24ae](https://github.com/goproxy/goproxy/commit/fca24ae2cd4e1ec66951ebf9396311e43ec05063))
* update release workflow to separate tagging and publishing ([#122](https://github.com/goproxy/goproxy/issues/122)) ([33c5b1e](https://github.com/goproxy/goproxy/commit/33c5b1e9a3991d6f067f6a98f5190407f31d9e95))
* update Test status badge in README.md ([3ab2b29](https://github.com/goproxy/goproxy/commit/3ab2b29a1abe97c9659cc0388be9276d2dc2012c))
* use `.yaml` as YAML file extension for GitHub Actions ([67628b3](https://github.com/goproxy/goproxy/commit/67628b3bd1fbb335db6771da951636eb622f346b))
* use `{{.Summary}}` as `snapshot.name_template` for .goreleaser.yaml ([9fec103](https://github.com/goproxy/goproxy/commit/9fec103f16f9cd66af3765b15495b8c8765afff3))
* use Alpine 3.18 as base image for Docker ([082b195](https://github.com/goproxy/goproxy/commit/082b1950f976595b28d786e1c880fa3a4fcbb94e))
* use Alpine 3.19 as base image for Docker ([e73f89b](https://github.com/goproxy/goproxy/commit/e73f89b3acfa3eb10a59a61d9289dd0ad00bffb9))
* use Go 1.19 for future releases ([c397ec0](https://github.com/goproxy/goproxy/commit/c397ec0be2730ac3821342341f24eb057174613e))
* use Go 1.23 for releases ([#78](https://github.com/goproxy/goproxy/issues/78)) ([0b35852](https://github.com/goproxy/goproxy/commit/0b35852a24e3199b6d822bb446e8efa0bf17adb7))
* use Go 1.24 for releases ([#97](https://github.com/goproxy/goproxy/issues/97)) ([8c974b5](https://github.com/goproxy/goproxy/commit/8c974b5b75a78a8106a874ff86e5a23b4d83dd86))
* use GoReleaser to release Docker image ([3dba219](https://github.com/goproxy/goproxy/commit/3dba2191cd92f995e8d2b346ec71d0c72a8d3063))
* use minio-go v6.0.56 ([ac0d46f](https://github.com/goproxy/goproxy/commit/ac0d46f6453b03789c39a129137df45abc071738))

## [0.20.2](https://github.com/goproxy/goproxy/compare/v0.20.1...v0.20.2) (2025-06-07)


### Miscellaneous Chores

* **deps:** bump golang.org/x/mod and github.com/minio/minio-go/v7 ([#123](https://github.com/goproxy/goproxy/issues/123)) ([4eee47d](https://github.com/goproxy/goproxy/commit/4eee47de7c80acba9f3138f4a6400a54fe62c987))
* update release workflow to separate tagging and publishing ([#122](https://github.com/goproxy/goproxy/issues/122)) ([33c5b1e](https://github.com/goproxy/goproxy/commit/33c5b1e9a3991d6f067f6a98f5190407f31d9e95))

## [0.20.1](https://github.com/goproxy/goproxy/compare/v0.20.0...v0.20.1) (2025-05-17)


### Code Refactoring

* utilize Go 1.22 for-range over integers ([#117](https://github.com/goproxy/goproxy/issues/117)) ([50d2fc6](https://github.com/goproxy/goproxy/commit/50d2fc6edadc7feb3d2f5a2414e6bee440574df0))


### Tests

* do not double-quote errors in logs ([#116](https://github.com/goproxy/goproxy/issues/116)) ([39e687a](https://github.com/goproxy/goproxy/commit/39e687ac5dc1ebc7cd0f38f785dc0867f84f89e1))


### Miscellaneous Chores

* **deps:** bump github.com/minio/minio-go/v7 from 7.0.88 to 7.0.91 ([#119](https://github.com/goproxy/goproxy/issues/119)) ([20411bd](https://github.com/goproxy/goproxy/commit/20411bdc6771f074785c7226310d0dc99d40ea21))
* **deps:** bump golang.org/x/crypto from 0.33.0 to 0.35.0 ([#113](https://github.com/goproxy/goproxy/issues/113)) ([ad197dd](https://github.com/goproxy/goproxy/commit/ad197dd3f86075a5aad25a22d6313a97f92247d8))
* **deps:** bump golang.org/x/net from 0.35.0 to 0.38.0 ([#115](https://github.com/goproxy/goproxy/issues/115)) ([239fd87](https://github.com/goproxy/goproxy/commit/239fd8753c2e9b0564d57f123fa721085e330da3))
* replace `interface{}` with `any` ([#118](https://github.com/goproxy/goproxy/issues/118)) ([86253b8](https://github.com/goproxy/goproxy/commit/86253b8a97adffeb89151b2799a81a97e7f81ff6))

## [0.20.0](https://github.com/goproxy/goproxy/compare/v0.19.2...v0.20.0) (2025-03-15)


### ⚠ BREAKING CHANGES

* **Goproxy:** redesign `ErrorLogger` as `Logger` using `log/slog.Logger` ([#106](https://github.com/goproxy/goproxy/issues/106))

### Features

* **cmd/goproxy:** add `--log-format` to `server` command ([#109](https://github.com/goproxy/goproxy/issues/109)) ([efa2ced](https://github.com/goproxy/goproxy/commit/efa2ced1005faab49a4828e86c31c724e8454a30))


### Code Refactoring

* bump minimum required Go version to 1.23.0 ([#112](https://github.com/goproxy/goproxy/issues/112)) ([f1c66d7](https://github.com/goproxy/goproxy/commit/f1c66d79c98bc2bf01eb44d66ab01e4e343bf314))
* **Goproxy:** redesign `ErrorLogger` as `Logger` using `log/slog.Logger` ([#106](https://github.com/goproxy/goproxy/issues/106)) ([ab925cf](https://github.com/goproxy/goproxy/commit/ab925cf087583688ac8745206355a5c53d6388cc))


### Tests

* improve test organization with subtests ([#110](https://github.com/goproxy/goproxy/issues/110)) ([5b2a4c8](https://github.com/goproxy/goproxy/commit/5b2a4c8ed731815ae519b8097987dbd62c99cbbe))


### Miscellaneous Chores

* **ci:** fix usage of codecov/codecov-action@v5 ([#108](https://github.com/goproxy/goproxy/issues/108)) ([bbf1666](https://github.com/goproxy/goproxy/commit/bbf1666550b726d7ab68a72fd62bcd6db6b6fe37))

## [0.19.2](https://github.com/goproxy/goproxy/compare/v0.19.1...v0.19.2) (2025-02-16)


### Miscellaneous Chores

* **.goreleaser.yaml:** add DOCKER_IMAGE_REPO for dynamic repo config ([#104](https://github.com/goproxy/goproxy/issues/104)) ([524fde2](https://github.com/goproxy/goproxy/commit/524fde25a2a7c41037201f63942ad0d1bb60fa72))
* **ci:** add build tests ([#105](https://github.com/goproxy/goproxy/issues/105)) ([92cac17](https://github.com/goproxy/goproxy/commit/92cac178cff71655131c3a27948fc92a6aeb7b43))
* **Dockerfile:** use `golang:1.24-alpine3.21` as base image ([#102](https://github.com/goproxy/goproxy/issues/102)) ([bae1a73](https://github.com/goproxy/goproxy/commit/bae1a7314993814495056b48629975cd5178c27f))

## [0.19.1](https://github.com/goproxy/goproxy/compare/v0.19.0...v0.19.1) (2025-02-16)


### Bug Fixes

* **cmd/goproxy:** require github.com/minio/crc64nvme@v1.0.1 ([#100](https://github.com/goproxy/goproxy/issues/100)) ([6e61c8f](https://github.com/goproxy/goproxy/commit/6e61c8fb617be2e69af7164e446364c3b06928da))

## [0.19.0](https://github.com/goproxy/goproxy/compare/v0.18.2...v0.19.0) (2025-02-16)


### Code Refactoring

* improve error message formatting ([#95](https://github.com/goproxy/goproxy/issues/95)) ([faf43bd](https://github.com/goproxy/goproxy/commit/faf43bd21170ae02274378cf75193c3cd8da0541))


### Tests

* cover Go 1.24 ([#96](https://github.com/goproxy/goproxy/issues/96)) ([d93abb4](https://github.com/goproxy/goproxy/commit/d93abb4bd1e107ad6c2369b3114736fca89273de))


### Miscellaneous Chores

* **.goreleaser.yaml:** align GORELEASER_ARTIFACTS_TARBALL with archive name template ([#89](https://github.com/goproxy/goproxy/issues/89)) ([fe067ab](https://github.com/goproxy/goproxy/commit/fe067abab77dcfa2a0caefb42adc01714f66eb03))
* **ci:** add support for `linux/riscv64` ([#94](https://github.com/goproxy/goproxy/issues/94)) ([cd425f3](https://github.com/goproxy/goproxy/commit/cd425f3907ea549342253d2fc08bdfa16382b265))
* **ci:** bump codecov/codecov-action from 4 to 5 ([#91](https://github.com/goproxy/goproxy/issues/91)) ([ab618b0](https://github.com/goproxy/goproxy/commit/ab618b0b09f0b9c1f4c80b5a00a7cc37d56f2666))
* **deps:** bump golang.org/x/crypto from 0.28.0 to 0.31.0 ([#92](https://github.com/goproxy/goproxy/issues/92)) ([6014fda](https://github.com/goproxy/goproxy/commit/6014fda90cce0891c9f11ab044ed7e6c66acdf09))
* **deps:** bump golang.org/x/mod, github.com/spf13/cobra, github.com/minio/minio-go/v7 ([#98](https://github.com/goproxy/goproxy/issues/98)) ([e75760c](https://github.com/goproxy/goproxy/commit/e75760c27ff1a22cda603f83b324cce8c3d9f5bc))
* **deps:** bump golang.org/x/net from 0.30.0 to 0.33.0 ([#93](https://github.com/goproxy/goproxy/issues/93)) ([093e27c](https://github.com/goproxy/goproxy/commit/093e27cfad43eb5d6ba0b6ecccc7a2edb23045d3))
* release 0.19.0 ([#99](https://github.com/goproxy/goproxy/issues/99)) ([6ea2ff0](https://github.com/goproxy/goproxy/commit/6ea2ff06922eaa0879035ff78e392b3a3fdabb9d))
* use Go 1.24 for releases ([#97](https://github.com/goproxy/goproxy/issues/97)) ([8c974b5](https://github.com/goproxy/goproxy/commit/8c974b5b75a78a8106a874ff86e5a23b4d83dd86))

## [0.18.2](https://github.com/goproxy/goproxy/compare/v0.18.1...v0.18.2) (2024-12-07)


### Bug Fixes

* **Dockerfile:** extract binaries from tarball when using GoReleaser artifacts ([#87](https://github.com/goproxy/goproxy/issues/87)) ([2b9d89c](https://github.com/goproxy/goproxy/commit/2b9d89c41e3724b4718637935693d59e6c94df34))

## [0.18.1](https://github.com/goproxy/goproxy/compare/v0.18.0...v0.18.1) (2024-12-07)


### Code Refactoring

* utilize `slices` package from stdlib ([#86](https://github.com/goproxy/goproxy/issues/86)) ([b108687](https://github.com/goproxy/goproxy/commit/b108687b51813c7110fde0b6309876f278f6e09a))


### Documentation

* **README.md:** add Conventional Commits requirement to "Contributing" section ([#81](https://github.com/goproxy/goproxy/issues/81)) ([c0ce09d](https://github.com/goproxy/goproxy/commit/c0ce09d6e384a61f7f012589da508f3d48cd738b))


### Miscellaneous Chores

* **ci:** configure release-please to open PRs as drafts ([#83](https://github.com/goproxy/goproxy/issues/83)) ([320a8c1](https://github.com/goproxy/goproxy/commit/320a8c17837c44373511372ac3750bb5d8b25bfe))
* **deps:** bump golang.org/x/mod and github.com/minio/minio-go/v7 ([#85](https://github.com/goproxy/goproxy/issues/85)) ([f44b882](https://github.com/goproxy/goproxy/commit/f44b8827e37dd3636606a42649af7d3750ecc6e3))
* **Dockerfile:** use Alpine 3.21 as base image ([#84](https://github.com/goproxy/goproxy/issues/84)) ([7bb9dfd](https://github.com/goproxy/goproxy/commit/7bb9dfd090ab4faa4f0abccc65f0abdbef942542))

## [0.18.0](https://github.com/goproxy/goproxy/compare/v0.17.2...v0.18.0) (2024-10-26)


### Bug Fixes

* **GoFetcher:** include `latest` query when performing `directList` ([#73](https://github.com/goproxy/goproxy/issues/73)) ([9dad311](https://github.com/goproxy/goproxy/commit/9dad311a82c3984a083ff0598cbed212ea7db38e))


### Code Refactoring

* bump minimum required Go version to 1.22.0 ([#79](https://github.com/goproxy/goproxy/issues/79)) ([7e4176b](https://github.com/goproxy/goproxy/commit/7e4176be1f233a2e069f6313e6ce5407bf2ec05a))


### Tests

* cover Go 1.23 ([#77](https://github.com/goproxy/goproxy/issues/77)) ([b8da543](https://github.com/goproxy/goproxy/commit/b8da543f31677edc2901aedc8a056477a7949c78))


### Miscellaneous Chores

* bump `.goreleaser.yaml` to v2 ([#75](https://github.com/goproxy/goproxy/issues/75)) ([7a75593](https://github.com/goproxy/goproxy/commit/7a75593fc37b82406c3db882bb864dbeb4ebc60c))
* release 0.18.0 ([#80](https://github.com/goproxy/goproxy/issues/80)) ([c985dba](https://github.com/goproxy/goproxy/commit/c985dbaa2025098fa1b671f8366122ecc31bbc33))
* use Go 1.23 for releases ([#78](https://github.com/goproxy/goproxy/issues/78)) ([0b35852](https://github.com/goproxy/goproxy/commit/0b35852a24e3199b6d822bb446e8efa0bf17adb7))

## [0.17.2](https://github.com/goproxy/goproxy/compare/v0.17.1...v0.17.2) (2024-07-09)


### Miscellaneous Chores

* release 0.17.2 ([#70](https://github.com/goproxy/goproxy/issues/70)) ([5bf903a](https://github.com/goproxy/goproxy/commit/5bf903a6a3509c8607b8c1f9bca92b6fa92eb3ce)), closes [#57](https://github.com/goproxy/goproxy/issues/57)

## [0.17.1](https://github.com/goproxy/goproxy/compare/v0.17.0...v0.17.1) (2024-07-05)


### Miscellaneous Chores

* **deps:** bump golang.org/x/mod from 0.18.0 to 0.19.0 ([#68](https://github.com/goproxy/goproxy/issues/68)) ([141fb73](https://github.com/goproxy/goproxy/commit/141fb73d2e6055df46cb99df1b0ac6fba1b15090))

## [0.17.0](https://github.com/goproxy/goproxy/compare/v0.16.10...v0.17.0) (2024-06-23)


### Features

* **Cacher:** support optional `interface{ Size() int64 }` for Content-Length header ([#60](https://github.com/goproxy/goproxy/issues/60)) ([546d218](https://github.com/goproxy/goproxy/commit/546d21817ed7ccf9fd925ee3262ce23dfa4aeb5c))


### Miscellaneous Chores

* **ci:** bump goreleaser/goreleaser-action from 5 to 6 ([#64](https://github.com/goproxy/goproxy/issues/64)) ([afa0f0b](https://github.com/goproxy/goproxy/commit/afa0f0b561da1dd88f9d96aef338df3ec5b6eb1c))
* **ci:** utilize googleapis/release-please-action@v4 ([#62](https://github.com/goproxy/goproxy/issues/62)) ([f2383d6](https://github.com/goproxy/goproxy/commit/f2383d6d93aeb5ed8a7528e1b0076ac7f09276e9))
* **deps:** bump github.com/spf13/cobra from 1.8.0 to 1.8.1 ([#65](https://github.com/goproxy/goproxy/issues/65)) ([39a876c](https://github.com/goproxy/goproxy/commit/39a876c6e55b84f77ebcab792bf7e1ea85a58022))
* **deps:** bump golang.org/x/mod from 0.16.0 to 0.18.0 ([#66](https://github.com/goproxy/goproxy/issues/66)) ([b4c1099](https://github.com/goproxy/goproxy/commit/b4c1099bf0ef93f953abff554eaae979343ee2cf))
* format README.md ([#61](https://github.com/goproxy/goproxy/issues/61)) ([0d2f7d6](https://github.com/goproxy/goproxy/commit/0d2f7d666a486ba7741fd3e39480dc9722a85e6b))
* release 0.17.0 ([#67](https://github.com/goproxy/goproxy/issues/67)) ([c688753](https://github.com/goproxy/goproxy/commit/c6887530ee86bbe7195f61af7002b6c358cc354b))
