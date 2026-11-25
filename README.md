# recgvmvp

I-O DATA の USB テレビチューナー GV-MVP/VZ と GV-MVP/HZ を Linux で使う、録画コマンドです。  
Go だけで書いた 1 つのバイナリで、cgo・libusb を使わず、Linux の `usbfs` で機器を直接操作します。  
本体のスロットに挿した B-CAS カードで復号された TS を出すので、外付けのカードリーダーや `b25` は不要です。

## 対応機種

| 機種 | USB ID | 放送波 | B-CAS | 詳細・動作確認 |
|---|---|---|---|---|
| I-O DATA GV-MVP/VZ | `04bb:052b` | 地デジ・BS・110 度 CS（1 チューナー） | 本体のスロット | [docs/GV-MVP-VZ.md](docs/GV-MVP-VZ.md) |
| I-O DATA GV-MVP/HZ | `04bb:0522` | 地デジ（1 チューナー） | 本体のスロット | [docs/GV-MVP-HZ.md](docs/GV-MVP-HZ.md) |

## 関連プロジェクト

- Windows（TVTest・EDCB）で使う BonDriver
  - GV-MVP/VZ: nuIlpointer 氏の [BonDriver_GVMVPVZ](https://github.com/nuIlpointer/BonDriver_GVMVPVZ)
  - GV-MVP/HZ: [BonDriver_GVMVPHZ](https://github.com/waya0125/BonDriver_GVMVPHZ)
- 手元のチューナーの動作確認の一覧: [チューナー動作確認一覧](https://waya0125.github.io/tuners/)

## 必要なもの

- 純正ドライバーに入っているファームウェア（電源を入れた直後の機器を使うとき）
  - GV-MVP/VZ: `NXP7164F_052B04BB.bin`
  - GV-MVP/HZ: `NXP7164F_052204BB.bin`
- 純正ドライバーの入手元（I-O DATA のサポートライブラリ）
  - GV-MVP/VZ: [製品ページ](https://www.iodata.jp/product/av/tidegi/gv-mvpvz/)・[サポートライブラリ](https://www.iodata.jp/lib/product/g/3338.htm)
  - GV-MVP/HZ: [製品ページ](https://www.iodata.jp/product/av/tidegi/gv-mvphz/)・[サポートライブラリ](https://www.iodata.jp/lib/product/g/2095.htm)
- BS・110 度 CS を受けるときは、アンテナへの電源（この機器は電源を出しません）

ファームウェアはメーカーの著作物なので、このリポジトリには含めていません。

## ビルド

```bash
go build -o bin/recgvmvp ./cmd/recgvmvp
go build -o bin/gvmvp-firmware ./cmd/gvmvp-firmware
go test ./...
```

手元に Go を入れない場合は、Docker の Go でビルドできます。

```bash
./scripts/go.sh build -o bin/recgvmvp ./cmd/recgvmvp
```

## 準備

### udev ルール（一般ユーザーで実行する場合）

```bash
sudo cp udev/99-gvmvpvz.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules
```

### ファームウェア

電源を入れた直後（コールド状態・DFU 状態）の機器を使う場合は、純正ドライバーからファームウェアを取り出して、次のどれかに置きます。  
上から順に探します。  
すでにウォーム状態なら不要です。

1. `--firmware` / `--fw` で指定したパス
2. 環境変数 `GVMVP_FW_PATH` / `GVMVPVZ_FW_PATH` / `GVMVPHZ_FW_PATH`
3. `recgvmvp` の実行ファイルと同じディレクトリ
4. `/usr/local/share/gvmvpvz/<ファイル名>`
5. `/lib/firmware/<ファイル名>`

付属の `gvmvp-firmware` コマンドで、ドライバーのフォルダーからの探索・SHA-256 の照合・配置ができます。

```bash
./bin/gvmvp-firmware verify /path/to/Driver
sudo ./bin/gvmvp-firmware install /path/to/Driver
```

## 使い方

```text
recgvmvp [オプション] チャンネル 秒 出力先
  チャンネル  13〜62（地デジ UHF）、BS01_0〜BS23_2（BS）、CS2〜CS24（110度CS）
  秒          録画秒数。'-' を指定すると SIGINT/SIGTERM まで連続受信
  出力先      出力ファイルパス。'-' を指定すると標準出力

オプション:
  --device PATH     使う機器ファイル（/dev/bus/usb/BBB/DDD）。空なら自動選択
  --model MODEL     使う機種（auto, vz, hz。既定: auto）
  --firmware PATH   ファームウェア（.bin）のパス（--fw も可）
  --tsid HEX        BS/CS の TSID を 16 進で明示指定（例: 0x40f1）。トランスポンダーに無ければエラー
  --check           選局してロック状態と受信目安を表示するだけ
  --reset           開く前に USB ポートリセットを発行する
  --debug           詳細ログを stderr に出力（GVMVPVZ_DEBUG=1 でも可）
  --b25 / --strip / --sid   recpt1 互換用オプション（受け付けるが無視）
```

### 実行例

```bash
# 地デジ 25ch の受信確認
recgvmvp --check 25 3

# 地デジ 25ch を 10 秒録画
recgvmvp 25 10 out.ts

# BS (NHK BS) を標準出力へ流す
recgvmvp BS15_0 - - > bs.ts

# 110度CS (CS4) を 10 秒録画
recgvmvp CS4 10 cs4.ts
```

### Mirakurun（`tuners.yml`）の設定例

VZ（BS・CS・地デジ）と HZ（地デジだけ）を同時につなぐ場合は、`--model` を指定するとチューナーを取り合いません。  
GV-MVP/VZ は 1 チューナーなので、1 台で地デジと BS・CS を同時には受けられません。  
VZ 1 台で地デジ・BS・CS を全部受ける場合は、`types: [GR, BS, CS]` を 1 つのエントリにまとめてください。

```yaml
- name: GV-MVP-VZ-BS
  types:
    - BS
    - CS
  command: recgvmvp --model vz <channel> - -
  decoder: ~
  isDisabled: false

- name: GV-MVP-HZ-GR
  types:
    - GR
  command: recgvmvp --model hz <channel> - -
  decoder: ~
  isDisabled: false
```

## 付属コマンド

| コマンド | 中身 |
|---|---|
| `cmd/recgvmvp` | 録画のコマンド（`recpt1` 互換の引数） |
| `cmd/gvmvp-firmware` | ファームウェアの検証・配置・手動転送 |
| `cmd/gvmvp-probe` | 低レベルの USB コマンド・B-CAS の状態の確認（開発用） |
| `cmd/gvmvp-tune` | 選局・ロックの確認（開発用） |

## 注意事項

- **BS・CS のアンテナ電源**: この機器はアンテナ線に電源を出しません。  
  BS・CS を受けるときは、分配器やほかの機器からアンテナに電源が出ている環境で使ってください。
- **BS のチャンネルの読み方**: `BSnn_s` の `s` は、そのトランスポンダーの何番目の TS か（recpt1・recisdb・Mirakurun の `channels.yml` と同じ）です。  
  選局したあと TMCC の TSID 一覧（空きを除く）の `s` 番目を選びます。頼んだ TS が無いときは、別の TS を出さずにエラーで止まります。
- **強制終了**: `kill -9` などで止めると、機器側の WatchDog でロックされることがあります。  
  ふだんは `SIGINT` / `SIGTERM` で止めてください。  
  ロックされた場合は、`--reset` か USB の抜き差しで戻ります。
- **同時接続の電源**: VZ・HZ ともバスパワーで最大 500mA を使います。  
  ハブ経由で同時につなぐ場合は、電源の容量に気をつけてください。

## ライセンス・出典

- MIT License（[LICENSE](./LICENSE)）
- IVDRM のセッション制御と TS の復号の手順は、nuIlpointer 氏の [BonDriver_GVMVPVZ](https://github.com/nuIlpointer/BonDriver_GVMVPVZ)（MIT）と Linux 移植版を参考にしています。
