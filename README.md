based on https://www.hwchiu.com/docs/2018/introduce-cni-iii

Invocation:
```
sudo CNI_COMMAND=ADD CNI_CONTAINERID=ns1 CNI_NETNS=/var/run/netns/ns1 CNI_IFNAME=eth10 CNI_PATH=`pwd` ./example < config
```
Hay que asegurarse de que el net namespace **ns1** existe:<br/>
```
sudo ip netns add ns1
```

Tambien hay que asegurar que el ejecutable esta en la ruta especificada en la variable de entorno CNI_PATH pasada en la invocación del comando.

Para construir: 
```
go build -o example
```