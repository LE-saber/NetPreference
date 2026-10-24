# NetPreference

[English](README.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [日本語](README.ja.md)

**ImmortalWrt / OpenWrt** 向けのデバイス別 DNS ポリシーと IPv4/IPv6 優先制御です。Go サービスと LuCI UI を備え、上限付きの A/AAAA 応答タイミングと DNS ルールを選択した機器に適用します。未選択の機器は既存の DNS 経路を使います。

**リリース：**[v0.1.0 MVP](https://github.com/LE-saber/NetPreference/releases/tag/v0.1.0)、パッケージ `0.1.0-r6`。対象は OpenWrt/ImmortalWrt 24.10 x86_64 です。リリースには ImmortalWrt 24.10.4 がテスト対象として記載されていますが、各ルーター環境では受け入れ確認が必要です。現在の LuCI UI は簡体字中国語で、この README 翻訳は UI を変更しません。

## 機能

- MAC と IPv4/IPv6 アドレスを識別し、デバイスごとに通常のデュアルスタック、IPv6 優先、IPv4 優先、任意のタイミング、DNS ブロックを指定。
- デバイス固有または登録済み全デバイス向けのドメインルール。完全一致/サブドメイン、A/AAAA/HTTPS/SVCB を選び、転送、静的書き換え、NXDOMAIN/NODATA/REFUSED、sinkhole、アドレス系統別フィルタリングを適用。
- 任意の IPv4/IPv6 送受信レート、セッション累計、IPv6 比率を **NetPreference 自身の nft カウンター**から取得。nlbwmon は不要です。監視やサービスのカウンターリセットで累計は戻り、offload により過小集計される場合があります。
- Apply/Restore、非公開の初期状態記録、所有権付き nftables テーブル、期限付き有効化セット、独立 watchdog、限定的な conntrack 清掃、削除前の復元。

「グローバル」は有効で登録済みの機器だけを対象とします。IPv4 専用アプリを IPv6 に強制変更するものではなく、NAT64 や帯域制限機能でもありません。

## DNS 経路と範囲

| 機器 | DNS 経路 |
| --- | --- |
| 未登録 | 既存経路。通常は `dnsmasq :53` |
| 登録済み・識別済み | `NetPreference :1053` → 元のリゾルバー、既定 `127.0.0.1:53` |
| 既存上流の例 | `dnsmasq` → HomeProxy/sing-box → SmartDNS → 上流 |

信頼する LAN インターフェースで MAC と送信元 IP を照合し、UDP/TCP 53 を転送します。ルーター自身の DNS は対象外で、既存の HomeProxy、SmartDNS、dnsmasq、NAT66、nlbwmon 設定を書き換えません。新しい IPv6 プライバシーアドレスは識別されるまで元の経路を使います。DoH、DoT、VPN 内の DNS、53 番以外は範囲外です。先行する DNS/TPROXY ルールやゲストネットワークの INPUT 制限は互換性に影響します。

## インストールと有効化

[v0.1.0](https://github.com/LE-saber/NetPreference/releases/tag/v0.1.0) から `luci-app-netpreference_0.1.0-r6_x86_64.ipk` と `SHA256SUMS` を取得し、対応ルーターの `/tmp` に置きます。

```sh
cd /tmp
sha256sum -c SHA256SUMS
opkg update
opkg install ./luci-app-netpreference_0.1.0-r6_x86_64.ipk
```

依存パッケージ：`luci-base`、`rpcd`、`uci`、`firewall4`、`nftables-json`、`ip-full`、`conntrack`。ファームウェアと一致する配布元を使い、依存関係の無視や不一致のカーネルパッケージを強制しないでください。nlbwmon の導入・削除は行いません。

**LuCI → Services / 服务 → NetPreference** を開きます。導入直後は待機状態で、登録機器はありません。上流を `127.0.0.1:53` に保ち、LAN インターフェース（既定 `br-lan`）を確認し、まず重要でないテスト機器を 1 台追加して保存・適用します。Restore ボタンで独自の転送設定を解除できます。

## タイミングの意味

**A** は非優先クエリの到着から、優先するアドレスの証拠を待つ最大時間です。**B** は優先アドレスの存在を確認した場合だけ追加する遅延です。合計待機には上限があり、AAAA がない場合に有効な A 応答を永久に抑止しません。

| プリセット | A | B |
| --- | ---: | ---: |
| Gentle | 75 ms | 40 ms |
| Balanced | 120 ms | 80 ms |
| Strong | 250 ms | 150 ms |

任意設定は `A ≤ 750 ms`、`B ≤ 500 ms`、`A+B ≤ 1000 ms`。IPv4 優先も対称です。能動問い合わせは DNS レコードを調べるもので、IPv6 の実到達性テストではありません。Happy Eyeballs、アプリのキャッシュ、暗号化 DNS、HTTPS/SVCB ヒントが結果に影響します。

## 復元と削除

```sh
netpreference restore
/etc/init.d/netpreference stop
opkg remove luci-app-netpreference
```

デーモンが利用できない場合も `restore` はオフライン復旧を試みます。通常のサービス停止は転送を解除し、次回起動時の設定意図を保持します。削除前フックは所有する状態を先に復元し、失敗した場合は復旧プログラムの削除を拒否します。

復元対象は所有権が確認できるルールと限定した DNS 転送接続で、古い設定全体を書き戻しません。`/etc/netpreference/baseline.json` は root 専用の監査記録として残り、元設定の機密情報を含み得るため公開しないでください。復旧はカーネル、nftables、conntrack、watchdog、元のリゾルバーにも依存します。デーモンと watchdog を同時終了すると、既存 conntrack セッションは期限切れを待つ場合があります。[構成と制限](docs/ARCHITECTURE.md)を参照してください。

## 開発と検証記録

開発チェックには Go 1.23+、Python 3、Node.js を使います。Go モジュールに第三者依存はありません。

```sh
bash scripts/check.sh
python3 scripts/build_ipk.py
```

出力先は `dist/` です。[SDK Makefile](openwrt/luci-app-netpreference/Makefile) が feed 統合に対応します。`sudo python3 tests/netns.py` と `sudo bash tests/rootfs_install.sh` は、必要な権限を持つ使い捨て Linux テスト環境で実行してください。

[実行記録](docs/EXECUTION.md) · [検証台帳](docs/VERIFICATION.md) · [失敗記録](docs/failures/)

これらは以前の開発段階の記録で、一部に nlbwmon を使う旧設計の記述があります。現在の流量処理は [traffic.go](internal/netpref/traffic.go) と [LuCI UI](files/www/luci-static/resources/view/netpreference/overview.js) を参照してください。今回の文書更新では対象ルーターの受け入れテストを行っていません。自動化された実行時テストの結果は、この PR の [GitHub Actions チェック](https://github.com/LE-saber/NetPreference/pull/3/checks)を参照してください。

MIT License：[LICENSE](LICENSE)。組み込む Go 標準ライブラリには別の [BSD ライセンス](docs/LICENSE-Go)があります。
