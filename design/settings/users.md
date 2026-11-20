# 利用者

誰が利用者（`user`）で、repository ごとの操作を誰に許すかを確かめるときに読む。
利用者という役割は、[役割](../../docs/roles/README.md#利用者) に書く。

## 利用者の決め方

- Logiforge は、利用者の名簿を持たない。GitForge のリポジトリの権限から決める。
  - なぜ：誰が書けるかの正本は GitForge にあるから。名簿を持つと、GitForge の権限と食い違い、2 か所を直すことになる。
- 次の両方を満たす主体が、`user` である。
  - WebUI のサインインで得たトークンを持つ（下の「トークンの出どころ」）。
  - 登録した repository のどれかに、Write 以上の権限がある。
- それ以外の主体は、`client` である。
- 最初の利用者は、初回のセットアップをした人である（[初回のセットアップ](setup.md)）。
  - セットアップでは、最初の repository の登録まで済ませる。そのあとは、この決め方で判定する。
- 利用者は、設定を変えられる（[設定の画面](../../docs/webui/settings.md)）。役割は 1 つだけで、利用者のほかに設定を変える役割はない。

## repository ごとの権限

- repository ごとの `user` の操作は、主体がその repository に Write 以上の権限を持つときだけ受け付ける。持たなければ `forbidden`（403）を返す。
  - 操作：人への依頼に回答する、repository を作る、apply request をレビューする（approve なら反映まで。[WebUI からのレビュー](#webui-からのレビュー)）。
  - 設定の API も、`user` であることを求める（[設定の API](../api/endpoints/settings.md)）。
- 読む操作は、`user` でも `client` でも、主体がその repository を読めれば受け付ける（[認可](../backend/README.md#認可)）。
- `client` の書き込み（apply request、レビュー、人への依頼の起票）は、GitForge の権限で決まる。Logiforge は、主体のトークンで書くので、GitForge が断れば断る（[GitForge のポートを pull request と issue で実装する](../adapters/pull-requests.md)）。
  - ディレクトリの repository では、そのディレクトリを置いた git リポジトリの権限を見る。
- 構成ごとの確かめ方：
  - GitHub：主体のトークンで `GET /user/installations` と `GET /user/installations/{installation_id}/repositories` を呼び、登録した GitHub リポジトリの `permissions.push` を見る（[GitHub アダプタの認証](../adapters/github.md#認証)）。
    - user access token で読めるのは、App が install されていて、かつ本人が読めるリポジトリだけである。そのため、App が install されていないリポジトリでは、`user` にならない。
  - Forgejo：主体のトークンで `GET /user/teams` を呼び、設定した organization の team の権限（`write`、`admin`、`owner`）を見る。repository ごとの操作は、`GET /repos/{owner}/{repo}` の `permissions.push` を見る（[Forgejo アダプタの認証](../adapters/forgejo.md#認証)）。
    - Forgejo の repository を作るときは、organization に repository を作れる team にいることを見る。なぜ：作る前の repository には、権限がないから。
    - 作るとき以外は、主体のトークンで `GET /repos/{owner}/{repo}` が 404 なら、書けないとみなす。なぜ：主体の team にその repository が入っていないときも、404 になるから。

## 設定を変えられる人

- 登録した repository のどれかに Write がある `user` は、設定も変えられる。
- そのため、登録した repository に Write を配ることは、Logiforge の設定を変える権限も渡すことになる。Write は、信頼できる人にだけ配る。
- エージェントの機械用アカウントは、WebUI でサインインしない。これは運用の決まりである。Logiforge は、機械用アカウントかを見分けない。
  - 機械用アカウントが持つのは CLI のトークンで、設定の API は CLI のトークンを断る（[トークンの出どころ](#トークンの出どころ)）。
- 誰が、いつ、どの設定を変えたかは、記録に残る（[記録](#記録)）。

## WebUI からのレビュー

- `user` は、WebUI で、apply request をレビューできる。CLI のレビューと同じ API と、同じ意味である（[apply request をレビューする](../api/endpoints/apply-request/review.md)）。
  - approve で反映の条件を満たしたら、同じ要求の中で反映する（[反映の手順](../backend/merge.md)）。
- 書くのは、サインインした本人の user access token である。merge も、本人のトークンで行う。
- その repository に Write がなければ、`forbidden` を返す。
- WebUI は、approve の前に、CLI の reviewer と同じレビューの材料（`getReviewMaterial`）を示す。自分が作った apply request なら、そのことも示す（[apply request と履歴](../../docs/webui/history.md)）。
- 人については、セッションの分離は、文脈の分離を保証しない（[保証の範囲](../../docs/apply-request/review.md#保証の範囲)）。

## トークンの出どころ

- WebUI のサインインで得たトークンかは、構成ごとに確かめる。
  - GitHub：`POST /applications/{client_id}/token` を、Logiforge の GitHub App の client_id と client secret で呼ぶ（[GitHub アダプタの認証](../adapters/github.md#認証)）。
  - Forgejo：`POST /login/oauth/introspect` を、Logiforge の OAuth2 のアプリケーションの client_id と client secret で呼び、`active` と `aud` で確かめる（[Forgejo アダプタの認証](../adapters/forgejo.md#認証)）。
- なぜ確かめるか：CLI のトークン（`gh auth login` のものなど）を盗まれても、設定を変えたり、`user` の書き込みをしたりできないようにするため。CLI の主体は、いつも `client` である。

## 権限のキャッシュ

- バックエンドは、権限を問い合わせた結果を、プロセスのメモリに 60 秒だけ持つ。
  - 鍵は、トークンの SHA-256 と、repository の名前である。トークンそのものは持たない。
  - なぜ持つか：`user` かどうかの判定に、登録した repository の数に応じた REST の呼び出しが要るから。要求ごとに呼ぶと、主体ごとの REST の上限に当たる（[費用と規模の見積もり](../estimates/README.md#github-アダプタ)）。
  - なぜ 60 秒か：GitForge で権限を外したあと、Logiforge で使えなくなるまでの遅れを、1 分までに収めるため。
  - これは、[状態を持たない](../backend/README.md#状態を持たない) の例外である。失っても、次の要求で問い合わせ直すだけである。
- 設定を変える要求では、キャッシュを使わずに問い合わせる。
  - なぜ：設定を変えられることは、ほかの操作より影響が大きいから。

## 締め出されたとき

- 利用者がいなくなったら（だれも登録した repository に書けない）、GitForge の側で、だれかに Write を戻す。
  - Logiforge に、締め出しを戻すためのコマンドはない。なぜ：権限の正本は GitForge にあるから。

## 記録

- 設定を変えたら、変えた利用者と時刻を、その行に残す（`updated_by`、`updated_at`）。
- バックエンドは、設定を変えるたびに、変えた設定の名前と利用者を、ログに出す。秘密の値は出さない。

## 対応しないこと

- 設定を変えられる人を、`user` の中でさらに絞ること。
  - なぜ：Logiforge は、利用者と設定を変える人を区別しないから。区別するには、名簿か、GitForge のより強い権限（organization の owner など）が要る。どちらも使わず、権限の正本を、登録した repository の Write の 1 つにするため。
