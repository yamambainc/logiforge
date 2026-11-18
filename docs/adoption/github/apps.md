# GitHub を使う構成で用意するもの

[Logiforge の repository を GitHub に置く](README.md) ときに、OAuth App、読むだけの GitHub App、署名の秘密を用意するために読む。

| もの | 作る人 | 使い道 |
|---|---|---|
| OAuth App | organization の owner | WebUI のサインイン。利用者を `user` とみなすのに使う |
| 読むだけの GitHub App | organization の owner | worker と、バックエンドの作業用の git の複製が、repository を読む |
| 署名の秘密（`LOGIFORGE_RECORD_KEYS`） | 管理者 | 記録の署名。バックエンドだけが持つ |

## OAuth App

organization の「Settings」→「Developer settings」→「OAuth Apps」で作る。

- Authorization callback URL：WebUI の `https://logiforge.example.com/oauth/callback`
- 作ったら、Client ID と client secret を控える。
- OAuth App がなくても、CLI（`client`）だけで使える。そのときは、`user` の主体がいないので、WebUI で repository を作ることと、人への依頼に答えることができない。

## 読むだけの GitHub App

organization の「Settings」→「Developer settings」→「GitHub Apps」→「New GitHub App」で作る。個人のリポジトリなら、個人の「Settings」→「Developer settings」で作る。

- GitHub App name：たとえば `logiforge-reader-acme`
- Homepage URL：Logiforge の URL か、この repository の URL
- Webhook：「Active」を外す
- Repository permissions：Contents、Issues、Pull requests を Read-only にする（Metadata は Read-only が既定で付く）。ほかは No access のまま
- Organization permissions と Account permissions：No access のまま
- Where can this GitHub App be installed?：Only on this account
- 作ったら、App ID を控え、「Private keys」→「Generate a private key」で秘密鍵（`.pem`）を落とす。秘密鍵は、サーバの秘密の置き場に移し、ほかに残さない。
- 「Install App」で、organization に install する。Repository access は「Only select repositories」にし、Logiforge の repository を選ぶ。
  - repository を足したら、install の設定でも選び足す。選んでいない repository は、worker が読めない（ログに `App が … に入っていない` と出る）。
  - 登録する repository の owner が複数なら、App を「Any account」にして、owner ごとに install する。
    - 「Any account」にすると、App は public になり、だれでも自分のアカウントに install できる。それでも問題はない。App は読むだけで、Logiforge は登録した owner の分しかトークンを作らないから。
- public のリポジトリだけなら、App がなくても動く。そのときは、トークンなしで読む。
  - ただし、トークンのない要求の上限は、1 時間に 60 回（送り元の IP ごと）である。反映かの判定で、`main` のコミットごとに REST を読むので、ふつうは足りない。試すときだけにする。
- private のリポジトリでは、要る。
