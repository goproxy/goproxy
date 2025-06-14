# Changelog

## [0.21.0](https://github.com/goproxy/goproxy/compare/v0.20.3...v0.21.0) (2025-06-14)


### Code Refactoring

* **cmd/goproxy:** simplify binary version handling ([#129](https://github.com/goproxy/goproxy/issues/129)) ([1b19b35](https://github.com/goproxy/goproxy/commit/1b19b35c1e5035036f4bd64716f637f3c78bd414))


### Miscellaneous Chores

* **deps:** bump github.com/minio/minio-go/v7 from 7.0.92 to 7.0.93 ([#132](https://github.com/goproxy/goproxy/issues/132)) ([6cb0544](https://github.com/goproxy/goproxy/commit/6cb054430423fa8305e1711a287169e0d0a5c661))
* **Dockerfile:** use `golang:1.24-alpine3.22` as base image ([#131](https://github.com/goproxy/goproxy/issues/131)) ([4c4128f](https://github.com/goproxy/goproxy/commit/4c4128f1aa74d2e2d023571cbec50026f8d45597))

## [0.20.3](https://github.com/goproxy/goproxy/compare/v0.20.2...v0.20.3) (2025-06-08)


### Miscellaneous Chores

* release 0.20.3 ([#127](https://github.com/goproxy/goproxy/issues/127)) ([927945e](https://github.com/goproxy/goproxy/commit/927945e93093bb454afd4c2497e3f54041ffc330))

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
