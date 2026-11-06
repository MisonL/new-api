package constant

// SessionKeyIDEpoch 是会话里记录的签发时用户会话世代。签发方（controller）与
// 校验方（middleware）必须使用同一个 key，放在这里避免两边各写一份字符串
// 导致吊销判定静默失效。
const SessionKeyIDEpoch = "id_epoch"
