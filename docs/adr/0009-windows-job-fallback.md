# Windows: reprodução com proteção reduzida quando o Job Object é recusado

O processo inicia sem alterar `CreationFlags` e sem pedir
`CREATE_BREAKAWAY_FROM_JOB`. Um job externo pode proibir breakaway e fazer o
próprio `CreateProcess` falhar com `ACCESS_DENIED`. A criação ocorre uma única
vez; erros nessa etapa continuam fatais.

Após iniciar, `startedGroup` tenta atribuir o processo ao nosso Job Object;
jobs aninhados podem aceitar isso. Esse caminho é compartilhado pela sessão e
pelos helpers de teste, inclusive quando chamam `cmd.Start()` diretamente.

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
