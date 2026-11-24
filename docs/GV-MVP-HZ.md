# I-O DATA GV-MVP/HZ

## 1. 概要

| 項目 | 内容 |
|---|---|
| USB ID | `04bb:0522`（USB 2.0 High-Speed、バスパワー最大 500mA。DFU 状態でも同じ ID） |
| 放送波 | 地デジ |
| チューナー数 | 1 |
| B-CAS | 本体のスロット（本体で復号するので、外付けのカードリーダー・`b25` は不要） |
| Linux | [recgvmvp](../README.md) |
| Windows | [BonDriver_GVMVPHZ](https://github.com/waya0125/BonDriver_GVMVPHZ) |
| 公式ページ | [製品ページ](https://www.iodata.jp/product/av/tidegi/gv-mvphz/)・[サポートライブラリ](https://www.iodata.jp/lib/product/g/2095.htm) |

## 2. 準備

1. udev ルールを置きます（[README の「準備」](../README.md#準備)）。
2. 純正ドライバーから `NXP7164F_052204BB.bin`（SHA-256: `162323cdb0fef9a5b0814a08a23dd8d437c2e9c0bc8803d88a9d2a4c0b91341f`）を取り出し、`gvmvp-firmware install` で置きます。  
   電源を入れた直後の機器にだけ要ります。

## 3. 使い方

```bash
recgvmvp --model hz --check 25 3     # 地デジ 25ch の受信確認
recgvmvp --model hz 25 10 out.ts     # 地デジ 25ch を 10 秒録画
recgvmvp --model hz 25 - -           # 標準出力に流し続ける（Mirakurun などから）
```

チャンネルは地デジの `13`〜`62` です。  
Mirakurun の設定例は [README](../README.md) にあります。

## 4. 動作確認の記録

2026-11-19 時点、Ubuntu 26.04（amd64）で確かめました。

| 確かめたこと | 結果 |
|---|---|
| GV-MVP/VZ との同時録画（Mirakurun 経由、同じ USB バス） | 両方とも連続カウンターの飛び 0、TS の同期の乱れ 0 で受信・解放できた |
| ウォーム状態からの開き直し | 問題なし |

## 5. 注意事項

- DFU 状態でも USB ID は `04bb:0522` のままです。  
  `recgvmvp` は USB の文字列記述子も見て、DFU 状態かどうかを決めています。
- VZ・HZ ともバスパワーで最大 500mA を使います。  
  ハブ経由で同時につなぐ場合は、電源の容量に気をつけてください。
- Mirakurun で VZ と一緒に使う場合は、`--model hz` を付けると、BS・CS の録画中に地デジ用のチューナーを取り合いません。
- `kill -9` などで止めると、機器側の WatchDog でロックされることがあります。  
  ロックされた場合は、`--reset` か USB の抜き差しで戻ります。

## 6. 技術メモ

この節は実装のときに調べた機器の中身の記録です。

### 6.1 ハードウェア構成

| 項目 | 内容 |
|---|---|
| USB ID | `04bb:0522`（USB 2.0 High-Speed） |
| ブリッジチップ | NXP SAA7164 USB 版 |
| チューナー・復調モジュール | `CTUNERID_8B_SAIGAWA`（`TunerID=0x8b` / 地上デジタル ISDB-T 専用、BS/110度CS 非対応） |
| チューナー構成 | **1T（地上デジタル専用シングルチューナー）** — 地上デジタル（ISDB-T）1 波のみ受信（BS/110度CS には非対応） |
| B-CAS | 本体スロット（`unit 9` `ISDBCASProp` / `IVDRMProp` 搭載） |
| ファームウェア | `NXP7164F_052204BB.bin`（サイズ `3,262,000` B、SHA-256: `162323cdb0fef9a5b0814a08a23dd8d437c2e9c0bc8803d88a9d2a4c0b91341f`） |

### 6.2 エンドポイントとコマンド形式

* **IF0 (`EP 0x01` OUT bulk / `EP 0x81` IN interrupt)**:
  * VZ（`04bb:052b`）と同じ SAA7164 11 バイト制御ヘッダ＋ペイロード（最大 256 バイト分割）。
  * `unit 0, sel=1`（`GetLen` size=4 → `400` バイト）から取得したデスクリプタおよび実機確認に基づく `unit` 割り当て:
    * `unit 2, 3`: システム / I2C（`GetLen(sel=0) = 256`）
    * `unit 4`: ISDB-T チューナー（`devicetype=0x40`, `TunerID=0x8b`）
    * `unit 5`: ISDB-T 復調器（`devicetype=0x100`, `TunerID=0x8b`）
    * `unit 8`: TS 出力ポート状態制御（`STOP=0`, `ACQUIRE=1`, `PAUSE=2`, `RUN=3`）
    * `unit 9`: IVDRM セッション・WatchDog・B-CAS 制御（VZ と共通）
* **IF1 (`EP 0x02` OUT bulk / `EP 0x82` IN interrupt)**
* **IF2 (`EP 0x03` OUT bulk / `EP 0x83` IN bulk)**:
  * MPEG-2 TS ストリーム出力（`EP 0x83` 非同期バルク受信）。

### 6.3 コールドブートとウォーム状態（VZ との重要な違い）

* **DFU（Boot2）状態でも `bcdDevice == 0x0100` を返す**:
  * 接続直後の `04bb:0522` は `bcdDevice=0x0100`、`iProduct="7164 CAP Boot2 High Speed - DFU "`、インタフェースクラス `0xFE`（DFU、`EP 0x03` OUT bulk, `wTransferSize=4096`）として列挙される。
  * VZ のように `bcdDevice == 0x0100` だけでウォーム判定すると DFU 転送をスキップしてしまうため、USB 文字列デスクリプタ（`iProduct` に `"DFU"` / `"Boot"` を含むか、`0x20` が `"01..."` か）を併せて判定する必要がある。
* **ファームウェア構造**:
  * `NXP7164F_052204BB.bin` は stage1（`130,880` B, offset `0x10`）＋ stage2（`3,131,104` B, offset `0x1ff50`）の構成。
  * Boot2 状態では stage2 を DFU 転送するとリセット・再列挙され、`iProduct="I-O DATA GV-MVP/HZ"`（`GET_FW_VERSION = 0x00021d0f`）で動作状態になる。

### 6.4 チューナー・復調器（Saigawa）の初期化と選局

* **GPIO 起床**:
  * `ExuGPIOControl`（`sel=1`）で `pin 0, 2, 3` が書き込み可能（`pin 1` は読み取り専用、`pin 4..31` は未実装）。
  * `ChannelDecoder_Saigawa::Init` に従い、`pin 0` を `0`（10 ms）→ `1`（2000 ms 待機）で起床させる。
* **未初期化状態での `I2CRead` に関する注意**:
  * DFU ブート直後・初期化前に `I2CRead(unit=4, reg=0x00, 1)` を実行すると、1 回目のみ `0x80` を返したあと同一ハンドル内でも 2 回目以降 I2C が NACK（`00000000` / `02000000`）になり、USB 抜き差しまで回復しない挙動を確認した。
  * 初期化前には `I2CRead` を行わず、必ず `GPIO` 起床の直後に `I2CWrite` による初期化を実行すること。
* **I2C 書き込み形式の違い**:
  * `unit 4`（チューナー側）: `I2CWrite(4, reg, []byte{val, 0xfe})`（データ長 2、末尾に `0xfe` を付加。初期化時 `0xff <- [0xff]`＋24 レジスタ）。
  * `unit 5`（復調器側）: `I2CWrite(5, reg, []byte{val})`（データ長 1。初期化時 94 レジスタ）。
* **選局とロック判定**:
  * `PhilMAS_052204BB.sys` 内に `0x71`（113）エントリの周波数テーブル（93.143 MHz 〜 767.143 MHz、各 20 バイト = `uint32 freqHz` ＋レジスタ設定値 15 バイト＋パディング 1 バイト）を持つ。
  * 選局時は `unit 4` へ固定ヘッダ＋テーブルの 15 レジスタ（`0x44, 0x45, 0x46, 0x49, 0x6f, 0x6e, 0x0e, 0x0f, 0x10, 0x11, 0x70, 0x6a, 0x6b, 0x6c, 0x6d`）＋`0x09 <- 0xf3` を書き込み、30 ms 待機後に `0x88 <- 0xfe`, `0x09 <- 0xf3`、最後に `unit 5` へ `0x01 <- 0x40` を書き込む。
  * ロック判定は `unit 5` の `reg 0x80`（`& 0x28 == 0`）かつ `reg 0xb0`（`& 0x0f >= 8`）。

### 6.5 USB ストリーム保護と WatchDog

* `unit 9` の 1024-bit Diffie-Hellman 鍵交換・AES-128-ECB 相互認証・WatchDog・`EP 0x83` の AES-128-CBC TS 復号は GV-MVP/VZ（`04bb:052b`）と完全に共通で動作する。

### 6.6 GV-MVP/VZ との同時運用（Mirakurun）

* 同一 USB バス上で GV-MVP/VZ（`04bb:052b`・BS）と GV-MVP/HZ（`04bb:0522`・地デジ）を Mirakurun 経由で同時録画し、両系統とも Continuity Counter ドロップ 0・TS 同期エラー 0 で受信・解放できることを確認済み。
* 両機とも `MaxPower 500mA` のバスパワー動作のため、ハブ等を介して同時接続する場合は電源容量に注意すること。
* `tuners.yml` では `--model vz` / `--model hz` を明示することで、BS/CS 録画中に地デジ用チューナーを取り合う問題を避けられる。
