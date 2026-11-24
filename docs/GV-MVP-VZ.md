# I-O DATA GV-MVP/VZ

## 1. 概要

| 項目 | 内容 |
|---|---|
| USB ID | `04bb:052b`（USB 2.0 High-Speed、バスパワー最大 500mA） |
| 放送波 | 地デジ・BS・110 度 CS |
| チューナー数 | 1（地デジと BS・CS は切り替えて使う） |
| B-CAS | 本体のスロット（本体で復号するので、外付けのカードリーダー・`b25` は不要） |
| アンテナ入力 | 地上・BS/CS 混合の 1 系統。BS・CS のアンテナに電源を出さない |
| Linux | [recgvmvp](../README.md) |
| Windows | nuIlpointer 氏の [BonDriver_GVMVPVZ](https://github.com/nuIlpointer/BonDriver_GVMVPVZ) |
| 公式ページ | [製品ページ](https://www.iodata.jp/product/av/tidegi/gv-mvpvz/)・[サポートライブラリ](https://www.iodata.jp/lib/product/g/3338.htm) |

## 2. 準備

1. udev ルールを置きます（[README の「準備」](../README.md#準備)）。
2. 純正ドライバーから `NXP7164F_052B04BB.bin` を取り出し、`gvmvp-firmware install` で置きます。  
   電源を入れた直後の機器にだけ要ります。  
   `recgvmvp` は、機器がコールド状態のときだけファームウェアを探して送ります。
3. BS・CS を受ける場合は、アンテナに電源が出ている環境にします。

## 3. 使い方

```bash
recgvmvp --model vz --check 25 3     # 地デジ 25ch の受信確認
recgvmvp --model vz 25 10 out.ts     # 地デジ 25ch を 10 秒録画
recgvmvp --model vz BS15_0 - - > bs.ts
recgvmvp --model vz CS4 10 cs4.ts
```

チャンネルの書き方は、地デジが `13`〜`62`、BS が `BS01_0`〜`BS23_2`、110 度 CS が `CS2`〜`CS24` です。  
Mirakurun の設定例は [README](../README.md) にあります。

## 4. 動作確認の記録

2026-11-19 時点、Ubuntu 26.04.1 LTS（amd64）で確かめました。

| 確かめたこと | 結果 |
|---|---|
| 60 秒の録画（地デジ・BS・110 度 CS） | どれもドロップ 0、TEI 0、同期ずれ 0 |
| PT3・torne との同じチャンネルの同時録画（10 秒・60 秒） | パケット数・ビットレート・PAT/PMT の構成が同じ |
| GV-MVP/HZ との同時録画（Mirakurun 経由、同じ USB バス） | どちらもドロップ 0 |
| 出力が始まるまでの時間（ウォーム状態） | 起動から約 0.1〜0.2 秒 |
| BS・CS ⇔ 地デジの切り替え、ウォーム状態からの開き直し | 問題なし |

## 5. 注意事項

- 1 チューナーなので、地デジと BS・CS を同時には受けられません。
- BS・CS のアンテナに電源を出しません。
- `kill -9` などで止めると、機器側の WatchDog でロックされることがあります。  
  ふだんは `SIGINT` / `SIGTERM` / `SIGHUP` で止めてください。  
  ロックされた場合は、`--reset` か USB の抜き差しで戻ります。

## 6. 技術メモ

この節は実装のときに調べた機器の中身の記録です。

### 6.1 ハードウェア構成

| 項目 | 内容 |
|---|---|
| USB ID | `04bb:052b`（USB 2.0 High-Speed） |
| ブリッジチップ | NXP SAA7164 USB 版 |
| チューナー・復調モジュール | Sony SMT-LJ100 / MJ110（地上デジタル ISDB-T・BS/110度CS ISDB-S 切替式） |
| チューナー構成 | **1TS（シングルチューナー）** — 地デジ・BS・110度CS を排他切替で 1 波のみ受信（地デジ＋BS/CS の同時受信には非対応） |
| B-CAS | 本体スロット挿入型（本体ハードウェアが MULTI2 復号を行うため、外部カードリーダー・`b25` 不要） |
| アンテナ入力 | 地上・BS/CS 混合 1 系統（**BS/CS アンテナへの LNB 電源供給機能なし**） |

### 6.2 エンドポイントとコマンド形式

* **IF0 (`EP 0x01` OUT bulk / `EP 0x81` IN interrupt)**:
  * SAA7164 の 11 バイト制御ヘッダ（`id`, `flags`, `size`, `command`, `controlselector`, `seqno`）＋ペイロード（最大 256 バイト分割）。
  * `unit` 割り当て:
    * `unit 2`: EEPROM / GPIO（リセット・電源制御）
    * `unit 4`: ISDB-S（BS/110度CS）復調・I2C 越しチューナー
    * `unit 5`: ISDB-T（地上デジタル）復調・I2C 越しチューナー
    * `unit 8`: TS 出力ポート状態制御（`STOP=0`, `ACQUIRE=1`, `PAUSE=2`, `RUN=3`）
    * `unit 9`: IVDRM セッション・WatchDog（`sel 0x00..0x08`）、B-CAS 制御（`sel 0x1e..0x22`）
* **IF2 (`EP 0x83` IN bulk)**:
  * MPEG-2 TS ストリーム出力。`recgvmvp` では `usbfs` の非同期 URB キュー（8 本 × 32 KiB）で受信する。

### 6.3 コールドブートとウォーム状態

* **コールド状態（`bcdDevice != 0x0100`）**:
  * 電源投入直後や USB 抜き差し直後は DFU モードで起動し、`NXP7164F_052B04BB.bin` の 2 ステージ転送が必要。
  * `recgvmvp` は `bcdDevice != 0x0100` を検出したときのみ自動でファームウェアを探して転送する。
* **ウォーム状態（`bcdDevice == 0x0100`）**:
  * すでにファームウェアが動作している状態。ファームウェアファイルが無くてもそのまま動作する。
  * 初回（`unit 5 reg 0x0f` が `0x34` でも `0x1c` でもない場合）のみ GPIO リセット・復調/チューナー初期化・内蔵 B-CAS 初期化を実行し、以降はウォーム再オープンする。
  * 地デジ有効時は `unit 5 reg 0x0f == 0x34`、BS/CS 有効時は `unit 5 reg 0x0f == 0x1c` となる。

### 6.4 ISDB-T（地上デジタル）と ISDB-S（BS・110度CS）の切り替え

* 本機は地デジと BS/CS の排他切り替え（1 チューナー）で動作する。
* **ISDB-T**: `13`〜`62` ch（UHF 中心周波数 `473.142857 MHz + (ch-13)*6 MHz`）。`unit 5` `reg 0x80`（`& 0x28 == 0`）と `reg 0xb0`（`& 0x0f >= 8`）でロックを判定し、`reg 0x8b..0x8d` から C/N 目安を算出する。
* **ISDB-S**: BS（`BS01_0`〜`BS23_2`）・110度CS（`CS2`〜`CS24`）。`unit 4` と `unit 4` 越しチューナーを初期化・起床し、局発 `10.678 GHz` 差分の IF 周波数（1040〜2060 MHz）と TSID（`reg 0x8f/0x90`）を設定する。`reg 0xc3`（bit4 == 0）でロックを判定し、`reg 0xce` からそのトランスポンダ内の有効 TSID 一覧を読み取れる。

### 6.5 USB ストリーム保護と WatchDog

* `unit 8` を `RUN` にする前に、`unit 9` で 1024-bit Diffie-Hellman 鍵交換と AES-128-ECB 相互認証を行い、`WatchDog`（`sel 0x06/0x07`）を開始する必要がある。
* 録画中は 2 秒周期で WatchDog キックを送信し続ける。
* `EP 0x83` から届く TS パケットは AES-128-CBC で保護されており、`PID 0x0000`（`tsc=1/2`, `afc=0x20`）で届く鍵パケット（`FORIVFROMNXP`）から TS 鍵・IV を復号して各パケット（`tsc=3`）を復号し、ヌルパケット（`PID 0x1FFF`）を除外して出力する。

### 6.6 運用上の注意

* **BS/CS 受信時のアンテナ給電**: 本機は BS/CS アンテナへ電源を供給しないため、常時給電のある分配器や他のチューナー・テレビ側から給電されている環境で利用する必要がある。
* **`kill -9` 等による強制終了**: WatchDog 停止や `unit 8 STOP`・セッション終了を行わずにプロセスを強制終了すると、機器側がロック状態になることがある。通常は `SIGINT` / `SIGTERM` / `SIGHUP` で終了し、ロックされた場合は `--reset` オプションまたは USB の抜き差しで回復できる。
