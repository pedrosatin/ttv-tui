# Login por device code grant, com refresh rotativo

O login usa device code grant (DCF) com um app registrado pelo operador: `ttv-tui
login` pede um `device_code`, mostra o `user_code` e abre `twitch.tv/activate`; o
usuário confirma no navegador e o app troca o código por access + refresh token. DCF
foi escolhido porque não precisa de redirect registrado, de porta local nem de client
secret — o que resolve os dois impasses do fluxo implícito (redirect fixo do client id
e token no fragmento da URL). Consequências registradas: access token vale 4 h, o
refresh token é de **uso único** (cada renovação invalida o anterior e devolve um novo)
e expira após 30 dias inativo, então o app precisa persistir o refresh rotacionado e
recomeçar o DCF quando ele expirar. O client id do app fica embutido no binário (é
público por natureza); quem compilar de outra origem passa o seu.
