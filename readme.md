## 生成密钥对

首先生成私钥 private.pem，然后根据私钥生成公钥 public.pem。这里的私钥 private.pem 是用于签名的，而公钥 public.pem 需要提供给服务端进行验证。

```bash
# 生成私钥
openssl genrsa -out private.pem 2048
# 生成公钥
openssl rsa -in private.pem -pubout -out public.pem
```

然后更新配置文件 agent.json，将 private_key_path 设置为上面生成的 private.pem 的**绝对路径**。

> 注意：这里不要设置 public_key_path 为 public.pem，按照下一节的要求配置为服务端的公钥，而不是本机生成的公钥。本机生成的公钥是提供给服务端验证用的。

## 配置服务器公钥

获取服务端公钥 server-public.pem，并将其绝对路径配置到 agent.json 中的 public_key_path。
