package io.gitpod.ide.jetbrains.backend.listeners

import com.intellij.ide.ApplicationInitializedListener
import com.intellij.openapi.application.ApplicationActivationListener
import com.intellij.openapi.components.ServiceManager
import io.gitpod.ide.jetbrains.backend.services.HeartbeatService
import com.intellij.openapi.wm.IdeFrame
import com.intellij.openapi.components.service

internal class MyApplicationActivationListener : ApplicationActivationListener {
    override fun applicationActivated(ideFrame: IdeFrame) {
        service<HeartbeatService>() // Services are not loaded if not referenced
    }
}
