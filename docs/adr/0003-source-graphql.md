# GraphQL do site atrás da interface Source, Helix depois

A navegação usa o GraphQL público do site porque funciona sem cadastro de app e as
queries necessárias foram validadas. É API interna e pode mudar sem aviso, então todo
acesso fica atrás da interface `Source`; a UI nunca importa HTTP nem conhece nomes de
campo. Um client id de app registrado na Twitch entra como adapter Helix atrás da
mesma interface — é o pré-requisito para "canais que eu sigo" de forma estável, e a
única mudança visível será a origem dos dados.
