# I-O DATA GV-MVP/HZ 技術メモ・デバイス特性

## 1. ハードウェア構成

| 項目 | 内容 |
|---|---|
| USB ID | `04bb:0522`（USB 2.0 High-Speed） |
| ブリッジチップ | NXP SAA7164 USB 版 |
| チューナー・復調モジュール | `CTUNERID_8B_SAIGAWA`（`TunerID=0x8b` / 地上デジタル ISDB-T 専用、BS/110度CS 非対応） |
| B-CAS | 本体スロット（`unit 9` `ISDBCASProp` / `IVDRMProp` 搭載） |
| ファームウェア | `NXP7164F_052204BB.bin`（サイズ `3,262,000` B、SHA-256: `162323cdb0fef9a5b0814a08a23dd8d437c2e9c0bc8803d88a9d2a4c0b91341f`） |

## 2. エンドポイントとコマンド形式

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

## 3. コールドブートとウォーム状態（VZ との重要な違い）

* **DFU（Boot2）状態でも `bcdDevice == 0x0100` を返す**:
  * 接続直後の `04bb:0522` は `bcdDevice=0x0100`、`iProduct="7164 CAP Boot2 High Speed - DFU "`、インタフェースクラス `0xFE`（DFU、`EP 0x03` OUT bulk, `wTransferSize=4096`）として列挙される。
  * VZ のように `bcdDevice == 0x0100` だけでウォーム判定すると DFU 転送をスキップしてしまうため、USB 文字列デスクリプタ（`iProduct` に `"DFU"` / `"Boot"` を含むか、`0x20` が `"01..."` か）を併せて判定する必要がある。
* **ファームウェア構造**:
  * `NXP7164F_052204BB.bin` は stage1（`130,880` B, offset `0x10`）＋ stage2（`3,131,104` B, offset `0x1ff50`）の構成。
  * Boot2 状態では stage2 を DFU 転送するとリセット・再列挙され、`iProduct="I-O DATA GV-MVP/HZ"`（`GET_FW_VERSION = 0x00021d0f`）で動作状態になる。

## 4. チューナー・復調器（Saigawa）の初期化と選局

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

## 5. USB ストリーム保護と WatchDog

* `unit 9` の 1024-bit Diffie-Hellman 鍵交換・AES-128-ECB 相互認証・WatchDog・`EP 0x83` の AES-128-CBC TS 復号は GV-MVP/VZ（`04bb:052b`）と完全に共通で動作する。
