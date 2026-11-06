# Windows: reprodução com proteção reduzida quando o Job Object é recusado

O processo pede `CREATE_BREAKAWAY_FROM_JOB`, preservando as demais flags. Se a
criação retornar `ACCESS_DENIED`, tenta novamente sem essa flag. Isso permite
rodar dentro de um job externo que proíbe breakaway. O processo continua sendo
atribuído ao nosso Job Object após iniciar; jobs aninhados podem aceitar isso.

Se a atribuição retornar `ACCESS_DENIED` para um processo ainda ativo, a sessão
continua com aviso na barra, uma vez por reprodução. Outros erros continuam
abortando o início. O log do CI sozinho não identifica a causa do acesso negado:
restrições de jobs externos e a janela de saída rápida precisam ser distinguidas.
Um processo já encerrado continua pelo fluxo normal de coleta do seu resultado.

Esta decisão substitui a recusa obrigatória do ticket 09 nesse caso específico.
Stop usa snapshots Toolhelp para descobrir descendentes e `TerminateProcess`
sobre handles mantidos pela sessão. Os handles impedem reutilização dos PIDs
conhecidos; tempos de criação rejeitam relações de parentesco antigas.

A descoberta é uma tentativa, não contenção: processos inacessíveis, criação de
filhos concorrente ao snapshot e descendentes de intermediários que já sumiram
podem escapar. Se a TUI morrer abruptamente, não há limpeza automática nesse modo.
O aviso expõe essa perda de proteção. Não se tenta matar um PID de registro antigo.

Quando Assign funciona, continua valendo `KILL_ON_JOB_CLOSE`: Stop termina o job,
e fechar o último handle pela morte da TUI termina seus membros no kernel.
A janela entre Start e Assign documentada no código continua existindo.

Referência: [Microsoft: AssignProcessToJobObject](https://learn.microsoft.com/en-us/windows/win32/api/jobapi2/nf-jobapi2-assignprocesstojobobject).
