# 例：Kubernetes の Pod

バックエンドと worker を、Kubernetes で動かすときに読む。
タスクの形は、[動かし方（コンテナ）](README.md) に従う。

2 つの鍵は Secret `logiforge`、設定の DB と置き場と clone は PersistentVolumeClaim に置く例である。

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: logiforge
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 10001
    fsGroup: 10001
    seccompProfile: { type: RuntimeDefault }
  containers:
    - name: backend
      image: logiforge:latest
      args: ["serve"]
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities: { drop: [ALL] }
      ports:
        - containerPort: 8080
      env:
        - { name: LOGIFORGE_ADDR, value: "0.0.0.0:8080" }
        - { name: LOGIFORGE_DB, value: /var/lib/logiforge/config/settings.db }
        - { name: LOGIFORGE_BACKEND_KEY_FILE, value: /run/secrets/logiforge/backend_key }
        - { name: LOGIFORGE_SHARED_KEY_FILE, value: /run/secrets/logiforge/shared_key }
        - { name: LOGIFORGE_STORE_DIR, value: /var/lib/logiforge/store }
      livenessProbe:
        httpGet: { path: /healthz, port: 8080 }
      readinessProbe:
        httpGet: { path: /healthz, port: 8080 }
      volumeMounts:
        - { name: config, mountPath: /var/lib/logiforge/config }
        - { name: store, mountPath: /var/lib/logiforge/store, readOnly: true }
        - { name: tmp, mountPath: /tmp }
        - { name: secrets, mountPath: /run/secrets/logiforge, readOnly: true }
    - name: worker
      image: logiforge:latest
      args: ["worker"]
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities: { drop: [ALL] }
      env:
        - { name: LOGIFORGE_DB, value: /var/lib/logiforge/config/settings.db }
        - { name: LOGIFORGE_SHARED_KEY_FILE, value: /run/secrets/logiforge/shared_key }
        - { name: LOGIFORGE_WORKER_DIR, value: /var/lib/logiforge/worker }
        - { name: LOGIFORGE_STORE_DIR, value: /var/lib/logiforge/store }
      livenessProbe:
        exec: { command: ["logiforge", "worker-health"] }
      volumeMounts:
        - { name: config, mountPath: /var/lib/logiforge/config, readOnly: true }
        - { name: worker, mountPath: /var/lib/logiforge/worker }
        - { name: store, mountPath: /var/lib/logiforge/store }
        - { name: tmp, mountPath: /tmp }
        - { name: shared-key, mountPath: /run/secrets/logiforge, readOnly: true }
  volumes:
    - name: worker
      persistentVolumeClaim: { claimName: logiforge-worker }
    - name: store
      persistentVolumeClaim: { claimName: logiforge-store }
    - name: config
      persistentVolumeClaim: { claimName: logiforge-config }
    - name: secrets
      secret: { secretName: logiforge }
    - name: shared-key
      secret:
        secretName: logiforge
        items: [{ key: shared_key, path: shared_key }]
    - name: tmp
      emptyDir: {}
```

- Pod を直接置くのは、例を短くするためである。動かすときは、レプリカを 1 にした StatefulSet か Deployment（`strategy: Recreate`）で包む。
  - なぜ `Recreate` か：入れ替えのときに、古い worker と新しい worker が同時に動かないようにするため。
- ルートのファイルシステムは、読むだけにする（`readOnlyRootFilesystem`）。書く場所は、ボリュームと、`/tmp` の `emptyDir` だけである。
  - バックエンドの `/tmp` には、作業用の git の複製を置く（[作業用の git の複製](../backend/apply-request/create.md#作業用の-git-の複製)）。
    - 目安の大きさは、repository の数 × clone 1 つの大きさである。clone は、artifact の中身を含むので、repository ごとに数十 MB から数百 MB である（[費用と規模の見積もり](../estimates/README.md#worker)）。repository 30 個で、数 GB になる。
    - 複製を消す仕組みは、作らない。`emptyDir` の大きさの上限（`sizeLimit`）は、目安に余裕を足して決める。
    - 上限を超えると、Pod が作り直される。複製は使い捨てのキャッシュなので、作り直された Pod は、要求ごとに空から clone し直す。そのあいだ、初めての `ar create` が遅くなるだけで、壊れるものはない。
    - バックエンドは、起動のときに、`LOGIFORGE_GIT_CACHE_DIR` の下の古い複製（`workcopy-*`）を消す。そのため、`LOGIFORGE_GIT_CACHE_DIR` は、プロセスごとの場所にする。1 台のマシンで、コンテナに分けずにバックエンドを何台か動かすときは、それぞれに別の場所を渡す。
- 設定の DB は、タスクの中で共有する永続のボリューム（`logiforge-config`）に置く（[タスクの形](README.md#タスクの形)）。
  - `logiforge-config` は、ブロックのストレージ（`ReadWriteOnce`）にする。NFS などのボリュームにしない（[設定の DB](../settings/database.md#db-の種類)）。
  - worker は、読むだけでマウントする。なぜ：worker に設定の DB を書かせないため（[設定の DB](../settings/database.md#db-の種類)）。
- worker には、Secret のうち共有の鍵（`shared_key`）だけをマウントする（`items`）。バックエンドの鍵を渡さない（[秘密の暗号化](../settings/secrets.md#2-つの鍵)）。
- worker の `livenessProbe` は、`exec` で `logiforge worker-health` を動かす（[ヘルスチェック](README.md#ヘルスチェック)）。
